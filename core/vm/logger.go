// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Storage represents a contract's storage.
type Storage map[common.Hash]common.Hash

// Copy duplicates the current storage.
func (s Storage) Copy() Storage {
	cpy := make(Storage, len(s))
	for key, value := range s {
		cpy[key] = value
	}
	return cpy
}

// Config are the configuration options for structured logger the EVM
type LogConfig struct {
	EnableMemory     bool // enable memory capture
	DisableStack     bool // disable stack capture
	DisableStorage   bool // disable storage capture
	EnableReturnData bool // enable return data capture
	Debug            bool // print output during capture end
	Limit            int  // maximum length of output, but zero means unlimited
	// Chain overrides, can be used to execute a trace using future fork rules
	Overrides *params.ChainConfig `json:"overrides,omitempty"`
}

//go:generate go run github.com/fjl/gencodec -type StructLog -field-override structLogMarshaling -out gen_structlog.go

// StructLog is emitted to the EVM each cycle and lists information about the current internal state
// prior to the execution of the statement.
// [kroma] : An error occurred when going:generate, so use the old code to restore it. StructLog.Memory, StructLog.ReturnData
type StructLog struct {
	Pc            uint64                      `json:"pc"`
	Op            OpCode                      `json:"op"`
	Gas           uint64                      `json:"gas"`
	GasCost       uint64                      `json:"gasCost"`
	Memory        []byte                      `json:"memory,omitempty"`
	MemorySize    int                         `json:"memSize"`
	Stack         []uint256.Int               `json:"stack"`
	ReturnData    []byte                      `json:"returnData,omitempty"`
	Storage       map[common.Hash]common.Hash `json:"-"`
	Depth         int                         `json:"depth"`
	RefundCounter uint64                      `json:"refund"`
	// [Scroll: START]
	ExtraData *types.ExtraData `json:"extraData"`
	// [Scroll: END]
	Err error `json:"-"`
}

// [Scroll: START]
var (
	loggerPool = sync.Pool{
		New: func() interface{} {
			return &StructLog{
				// init arrays here; other types are inited with default values
				Stack: make([]uint256.Int, 0),
			}
		},
	}
)

func NewStructlog(pc uint64, op OpCode, gas, cost uint64, depth int, err error) *StructLog {
	structlog := loggerPool.Get().(*StructLog)
	structlog.Pc, structlog.Op, structlog.Gas, structlog.GasCost, structlog.Depth, structlog.Err = pc, op, gas, cost, depth, err

	runtime.SetFinalizer(structlog, func(logger *StructLog) {
		logger.clean()
		loggerPool.Put(logger)
	})
	return structlog
}

func (s *StructLog) clean() {
	s.Stack = s.Stack[:0]
	s.Storage = nil
	s.ExtraData = nil
	s.Err = nil
}

func (s *StructLog) getOrInitExtraData() *types.ExtraData {
	if s.ExtraData == nil {
		s.ExtraData = &types.ExtraData{}
	}
	return s.ExtraData
}

// [Scroll: END]

// overrides for gencodec
type structLogMarshaling struct {
	Gas         math.HexOrDecimal64
	GasCost     math.HexOrDecimal64
	Memory      hexutil.Bytes
	ReturnData  hexutil.Bytes
	OpName      string `json:"opName"`          // adds call to OpName() in MarshalJSON
	ErrorString string `json:"error,omitempty"` // adds call to ErrorString() in MarshalJSON
}

// OpName formats the operand name in a human-readable format.
func (s *StructLog) OpName() string {
	return s.Op.String()
}

// ErrorString formats the log's error as a string.
func (s *StructLog) ErrorString() string {
	if s.Err != nil {
		return s.Err.Error()
	}
	return ""
}

// EVMLogger is used to collect execution traces from an EVM transaction
// execution. CaptureState is called for each step of the VM with the
// current VM state.
// Note that reference types are actual VM data structures; make copies
// if you need to retain them beyond the current call.
type EVMLogger interface {
	// Transaction level
	CaptureTxStart(gasLimit uint64)
	CaptureTxEnd(restGas uint64)
	// Top call frame
	CaptureStart(env *EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int)
	CaptureEnd(output []byte, gasUsed uint64, err error)
	// Rest of call frames
	CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int)
	CaptureExit(output []byte, gasUsed uint64, err error)
	// Opcode level
	CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)
	// [Scroll: START]
	CaptureStateAfter(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)
	// [Scroll: END]
	CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error)
}

// StructLogger is an EVM state logger and implements EVMLogger.
//
// StructLogger can capture state based on the given Log configuration and also keeps
// a track record of modified storage which is used in reporting snapshots of the
// contract their storage.
type StructLogger struct {
	cfg LogConfig
	env *EVM

	// [Scroll: START]
	statesAffected map[common.Address]struct{}
	// [Scroll: END]
	storage map[common.Address]Storage
	// [Scroll: START]
	createdAccount *types.AccountWrapper

	callStackLogInd []int
	// [Scroll: END]
	logs     []*StructLog
	output   []byte
	err      error
	gasLimit uint64
	usedGas  uint64

	interrupt uint32 // Atomic flag to signal execution interruption
	reason    error  // Textual reason for the interruption
}

// NewStructLogger returns a new logger
func NewStructLogger(cfg *LogConfig) *StructLogger {
	logger := &StructLogger{
		storage: make(map[common.Address]Storage),
		// [Scroll: START]
		statesAffected: make(map[common.Address]struct{}),
		// [Scroll: END]
	}
	if cfg != nil {
		logger.cfg = *cfg
	}
	return logger
}

// Reset clears the data held by the logger.
func (l *StructLogger) Reset() {
	l.storage = make(map[common.Address]Storage)
	// [Scroll: START]
	l.statesAffected = make(map[common.Address]struct{})
	// [Scroll: END]
	l.output = make([]byte, 0)
	l.logs = l.logs[:0]
	// [Scroll: START]
	l.callStackLogInd = nil
	// [Scroll: END]
	l.err = nil
	// [Scroll: START]
	l.createdAccount = nil
	// [Scroll: END]
}

// CaptureStart implements the EVMLogger interface to initialize the tracing operation.
func (l *StructLogger) CaptureStart(env *EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	l.env = env
	// [Scroll: START]
	if create {
		// notice codeHash is set AFTER CreateTx has exited, so here codeHash is still empty
		l.createdAccount = &types.AccountWrapper{
			Address: to,
			// nonce is 1 after EIP158, so we query it from stateDb
			Nonce:   env.StateDB.GetNonce(to),
			Balance: (*hexutil.Big)(value),
		}
	}

	l.statesAffected[from] = struct{}{}
	l.statesAffected[to] = struct{}{}
	// [Scroll: END]
}

// CaptureState logs a new structured log message and pushes it out to the environment
//
// CaptureState also tracks SLOAD/SSTORE ops to track storage change.
func (l *StructLogger) CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, opErr error) {
	// If tracing was interrupted, set the error and stop
	if atomic.LoadUint32(&l.interrupt) > 0 {
		l.env.Cancel()
		return
	}
	// check if already accumulated the specified number of logs
	if l.cfg.Limit != 0 && l.cfg.Limit <= len(l.logs) {
		return
	}

	memory := scope.Memory
	stack := scope.Stack
	contract := scope.Contract
	// [Scroll: START]
	// create a struct log.
	structlog := NewStructlog(pc, op, gas, cost, depth, opErr)
	// [Scroll: END]
	// Copy a snapshot of the current memory state to a new buffer
	if l.cfg.EnableMemory {
		// [Scroll: START]
		structlog.Memory = make([]byte, len(memory.Data()))
		copy(structlog.Memory, memory.Data())
		structlog.MemorySize = memory.Len()
		// [Scroll: END]
	}
	// Copy a snapshot of the current stack state to a new buffer
	if !l.cfg.DisableStack {
		// [Scroll: START]
		structlog.Stack = append(structlog.Stack, stack.Data()...)
		// [Scroll: END]
	}
	var (
		recordStorageDetail bool
		storageKey          common.Hash
		storageValue        common.Hash
	)
	stackData := stack.Data()
	stackLen := len(stackData)
	if !l.cfg.DisableStorage && (op == SLOAD || op == SSTORE) {
		// capture SLOAD opcodes and record the read entry in the local storage
		if op == SLOAD && stackLen >= 1 {
			// [Scroll: START]
			recordStorageDetail = true
			storageKey = stack.data[stack.len()-1].Bytes32()
			storageValue = l.env.StateDB.GetState(contract.Address(), storageKey)
			// [Scroll: END]
		} else if op == SSTORE && stackLen >= 2 {
			// [Scroll: START]
			recordStorageDetail = true
			storageKey = stack.data[stack.len()-1].Bytes32()
			storageValue = stack.data[stack.len()-2].Bytes32()
			// [Scroll: END]
		}
	}
	// [Scroll: START]
	if recordStorageDetail {
		contractAddress := contract.Address()
		if l.storage[contractAddress] == nil {
			l.storage[contractAddress] = make(Storage)
		}
		l.storage[contractAddress][storageKey] = storageValue
		structlog.Storage = l.storage[contractAddress].Copy()

		if err := traceStorage(l, scope, structlog.getOrInitExtraData()); err != nil {
			log.Error("Failed to trace data", "opcode", op.String(), "err", err)
		}
	}
	// [Scroll: END]
	if l.cfg.EnableReturnData {
		// [Scroll: START]
		structlog.ReturnData = make([]byte, len(rData))
		copy(structlog.ReturnData, rData)
		// [Scroll: END]
	}
	// [Scroll: START]
	execFuncList, ok := OpcodeExecs[op]
	if ok {
		// execute trace func list.
		for _, exec := range execFuncList {
			if err := exec(l, scope, structlog.getOrInitExtraData()); err != nil {
				log.Error("Failed to trace data", "opcode", op.String(), "err", err)
			}
		}
	}
	// for each "calling" op, pick the caller's state
	switch op {
	case CALL, CALLCODE, STATICCALL, DELEGATECALL, CREATE, CREATE2:
		extraData := structlog.getOrInitExtraData()
		extraData.Caller = append(extraData.Caller, getWrappedAccountForAddr(l, scope.Contract.Address()))
	}

	structlog.RefundCounter = l.env.StateDB.GetRefund()
	l.logs = append(l.logs, structlog)
	// [Scroll: END]
}

// [Scroll:START]
func (l *StructLogger) CaptureStateAfter(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
}

// [Scroll:END]

// CaptureFault implements the EVMLogger interface to trace an execution fault
// while running an opcode.
func (l *StructLogger) CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error) {
}

// CaptureEnd is called after the call finishes to finalize the tracing.
func (l *StructLogger) CaptureEnd(output []byte, gasUsed uint64, err error) {
	l.output = output
	// [Scroll: START]
	if err != nil {
		l.err = err
	}
	// [Scroll: END]
	if l.cfg.Debug {
		fmt.Printf("%#x\n", output)
		if err != nil {
			fmt.Printf(" error: %v\n", err)
		}
	}
}

func (l *StructLogger) CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	// [Scroll: START]
	// the last logged op should be CALL/STATICCALL/CALLCODE/CREATE/CREATE2
	lastLogPos := len(l.logs) - 1
	log.Debug("mark call stack", "pos", lastLogPos, "op", l.logs[lastLogPos].Op)
	l.callStackLogInd = append(l.callStackLogInd, lastLogPos)
	// sanity check
	if len(l.callStackLogInd) != l.env.depth {
		panic("unexpected evm depth in capture enter")
	}
	l.statesAffected[to] = struct{}{}
	theLog := l.logs[lastLogPos]
	theLog.getOrInitExtraData()
	// handling additional updating for CALL/STATICCALL/CALLCODE/CREATE/CREATE2 only
	// append extraData part for the log, capture the account status (the nonce / balance has been updated in capture enter)
	wrappedStatus := getWrappedAccountForAddr(l, to)
	theLog.ExtraData.StateList = append(theLog.ExtraData.StateList, wrappedStatus)
	// finally we update the caller's status (it is possible that nonce and balance being updated)
	if len(theLog.ExtraData.Caller) == 1 {
		theLog.ExtraData.Caller = append(theLog.ExtraData.Caller, getWrappedAccountForAddr(l, from))
	}
	// [Scroll: END]
}

// [Scroll: START]
// in CaptureExit phase, a CREATE has its target address's code being set and queryable
// [Scroll: END]
func (l *StructLogger) CaptureExit(output []byte, gasUsed uint64, err error) {
	// [Scroll: START]
	stackH := len(l.callStackLogInd)
	if stackH == 0 {
		panic("unexpected capture exit occur")
	}

	theLogPos := l.callStackLogInd[stackH-1]
	l.callStackLogInd = l.callStackLogInd[:stackH-1]
	theLog := l.logs[theLogPos]
	// update "forecast" data
	if err != nil {
		theLog.ExtraData.CallFailed = true
	}

	// handling updating for CREATE only
	switch theLog.Op {
	case CREATE, CREATE2:
		// append extraData part for the log whose op is CREATE(2), capture the account status (the codehash would be updated in capture exit)
		dataLen := len(theLog.ExtraData.StateList)
		if dataLen == 0 {
			panic("unexpected data capture for target op")
		}

		lastAccData := theLog.ExtraData.StateList[dataLen-1]
		wrappedStatus := getWrappedAccountForAddr(l, lastAccData.Address)
		theLog.ExtraData.StateList = append(theLog.ExtraData.StateList, wrappedStatus)
		code := getCodeForAddr(l, lastAccData.Address)
		theLog.ExtraData.CodeList = append(theLog.ExtraData.CodeList, hexutil.Encode(code))
	default:
		//do nothing for other op code
		return
	}
	// [Scroll: END]
}

func (l *StructLogger) GetResult() (json.RawMessage, error) {
	// Tracing aborted
	if l.reason != nil {
		return nil, l.reason
	}
	failed := l.err != nil
	returnData := common.CopyBytes(l.output)
	// Return data when successful and revert reason when reverted, otherwise empty.
	returnVal := fmt.Sprintf("%x", returnData)
	if failed && l.err != ErrExecutionReverted {
		returnVal = ""
	}
	return json.Marshal(&types.ExecutionResult{
		Gas:         l.usedGas,
		Failed:      failed,
		ReturnValue: returnVal,
		StructLogs:  FormatLogs(l.StructLogs()),
	})
}

// Stop terminates execution of the tracer at the first opportune moment.
func (l *StructLogger) Stop(err error) {
	l.reason = err
	atomic.StoreUint32(&l.interrupt, 1)
}

func (l *StructLogger) CaptureTxStart(gasLimit uint64) {
	l.gasLimit = gasLimit
}

func (l *StructLogger) CaptureTxEnd(restGas uint64) {
	l.usedGas = l.gasLimit - restGas
}

// [Scroll: START]
// UpdatedAccounts is used to collect all "touched" accounts
func (l *StructLogger) UpdatedAccounts() map[common.Address]struct{} {
	return l.statesAffected
}

// UpdatedStorages is used to collect all "touched" storage slots
func (l *StructLogger) UpdatedStorages() map[common.Address]Storage {
	return l.storage
}

// CreatedAccount return the account data in case it is a create tx
func (l *StructLogger) CreatedAccount() *types.AccountWrapper { return l.createdAccount }

// [Scroll: END]

// StructLogs returns the captured log entries.
func (l *StructLogger) StructLogs() []*StructLog { return l.logs }

// Error returns the VM error captured by the trace.
func (l *StructLogger) Error() error { return l.err }

// Output returns the VM return value captured by the trace.
func (l *StructLogger) Output() []byte { return l.output }

func (l *StructLogger) MaybeAddFeeRecipientsToStatesAffected(tx *types.Transaction) {
	if !tx.IsDepositTx() {
		l.statesAffected[params.KromaProtocolVault] = struct{}{}
		l.statesAffected[params.KromaProposerRewardVault] = struct{}{}
		l.statesAffected[params.KromaValidatorRewardVault] = struct{}{}
	}
}

// WriteTrace writes a formatted trace to the given writer
// [Scroll: START]
func WriteTrace(writer io.Writer, logs []*StructLog) {
	// [Scroll: END]
	for _, log := range logs {
		fmt.Fprintf(writer, "%-16spc=%08d gas=%v cost=%v", log.Op, log.Pc, log.Gas, log.GasCost)
		if log.Err != nil {
			fmt.Fprintf(writer, " ERROR: %v", log.Err)
		}
		fmt.Fprintln(writer)

		if len(log.Stack) > 0 {
			fmt.Fprintln(writer, "Stack:")
			for i := len(log.Stack) - 1; i >= 0; i-- {
				fmt.Fprintf(writer, "%08d  %s\n", len(log.Stack)-i-1, log.Stack[i].Hex())
			}
		}
		// [Scroll: START]
		if len(log.Memory) > 0 {
			fmt.Fprintln(writer, "Memory:")
			fmt.Fprint(writer, hex.Dump(log.Memory))
		}
		// [Scroll: END]
		if len(log.Storage) > 0 {
			fmt.Fprintln(writer, "Storage:")
			for h, item := range log.Storage {
				fmt.Fprintf(writer, "%x: %x\n", h, item)
			}
		}
		// [Scroll: START]
		if len(log.ReturnData) > 0 {
			fmt.Fprintln(writer, "ReturnData:")
			fmt.Fprint(writer, hex.Dump(log.ReturnData))
		}
		// [Scroll: END]
		fmt.Fprintln(writer)
	}
}

// WriteLogs writes vm logs in a readable format to the given writer
func WriteLogs(writer io.Writer, logs []*types.Log) {
	for _, log := range logs {
		fmt.Fprintf(writer, "LOG%d: %x bn=%d txi=%x\n", len(log.Topics), log.Address, log.BlockNumber, log.TxIndex)

		for i, topic := range log.Topics {
			fmt.Fprintf(writer, "%08d  %x\n", i, topic)
		}

		fmt.Fprint(writer, hex.Dump(log.Data))
		fmt.Fprintln(writer)
	}
}

type mdLogger struct {
	out io.Writer
	cfg *LogConfig
	env *EVM
}

// NewMarkdownLogger creates a logger which outputs information in a format adapted
// for human readability, and is also a valid markdown table
func NewMarkdownLogger(cfg *LogConfig, writer io.Writer) *mdLogger {
	l := &mdLogger{out: writer, cfg: cfg}
	if l.cfg == nil {
		l.cfg = &LogConfig{}
	}
	return l
}

func (t *mdLogger) CaptureStart(env *EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	t.env = env
	if !create {
		fmt.Fprintf(t.out, "From: `%v`\nTo: `%v`\nData: `%#x`\nGas: `%d`\nValue `%v` wei\n",
			from.String(), to.String(),
			input, gas, value)
	} else {
		fmt.Fprintf(t.out, "From: `%v`\nCreate at: `%v`\nData: `%#x`\nGas: `%d`\nValue `%v` wei\n",
			from.String(), to.String(),
			input, gas, value)
	}

	fmt.Fprintf(t.out, `
|  Pc   |      Op     | Cost |   Stack   |   RStack  |  Refund |
|-------|-------------|------|-----------|-----------|---------|
`)
}

// CaptureState also tracks SLOAD/SSTORE ops to track storage change.
func (t *mdLogger) CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
	stack := scope.Stack
	fmt.Fprintf(t.out, "| %4d  | %10v  |  %3d |", pc, op, cost)

	if !t.cfg.DisableStack {
		// format stack
		var a []string
		for _, elem := range stack.Data() {
			a = append(a, elem.Hex())
		}
		b := fmt.Sprintf("[%v]", strings.Join(a, ","))
		fmt.Fprintf(t.out, "%10v |", b)
	}
	fmt.Fprintf(t.out, "%10v |", t.env.StateDB.GetRefund())
	fmt.Fprintln(t.out, "")
	if err != nil {
		fmt.Fprintf(t.out, "Error: %v\n", err)
	}
}

// [Scroll:START]
// CaptureStateAfter for special needs, tracks SSTORE ops and records the storage change.
func (t *mdLogger) CaptureStateAfter(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
}

// [Scroll:END]

func (t *mdLogger) CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error) {
	fmt.Fprintf(t.out, "\nError: at pc=%d, op=%v: %v\n", pc, op, err)
}

func (t *mdLogger) CaptureEnd(output []byte, gasUsed uint64, err error) {
	fmt.Fprintf(t.out, "\nOutput: `%#x`\nConsumed gas: `%d`\nError: `%v`\n",
		output, gasUsed, err)
}

func (t *mdLogger) CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

func (t *mdLogger) CaptureExit(output []byte, gasUsed uint64, err error) {}

func (*mdLogger) CaptureTxStart(gasLimit uint64) {}

func (*mdLogger) CaptureTxEnd(restGas uint64) {}

// [Scroll: START]
// FormatLogs formats EVM returned structured logs for json output
func FormatLogs(logs []*StructLog) []*types.StructLogRes {
	formatted := make([]*types.StructLogRes, 0, len(logs))

	for _, trace := range logs {
		logRes := types.NewStructLogResBasic(trace.Pc, trace.Op.String(), trace.Gas, trace.GasCost, trace.Depth, trace.RefundCounter, trace.Err)
		for _, stackValue := range trace.Stack {
			logRes.Stack = append(logRes.Stack, stackValue.Hex())
		}
		for i := 0; i+32 <= len(trace.Memory); i += 32 {
			logRes.Memory = append(logRes.Memory, common.Bytes2Hex(trace.Memory[i:i+32]))
		}
		if len(trace.Storage) != 0 {
			storage := make(map[string]string)
			for i, storageValue := range trace.Storage {
				storage[i.Hex()] = storageValue.Hex()
			}
			logRes.Storage = storage
		}
		logRes.ExtraData = trace.ExtraData

		formatted = append(formatted, logRes)
	}
	return formatted
}

// [Scroll: END]
