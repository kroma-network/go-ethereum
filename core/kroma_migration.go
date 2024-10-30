package core

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

var HaltOnStateTransition = errors.New("historical rpc must be set to transition to MPT")

var (
	accountChangesPrefix  = []byte("aC-")
	storageChangesPrefix  = []byte("sC-")
	destructChangesPrefix = []byte("dC-")
	migratedRootKey       = []byte("MigratedRoot")
	migratedNumberKey     = []byte("MigratedNumber")
)

type MigratedRef struct {
	db ethdb.Database
	mu sync.Mutex

	root   common.Hash
	number uint64
}

func NewMigratedRef(db ethdb.Database) *MigratedRef {
	ref := &MigratedRef{db: db}
	if b, _ := db.Get(migratedRootKey); len(b) > 0 {
		ref.root = common.BytesToHash(b)
	}
	if b, _ := db.Get(migratedNumberKey); len(b) > 0 {
		ref.number = hexutil.MustDecodeUint64(string(b))
	}
	return ref
}

func (mr *MigratedRef) Update(root common.Hash, number uint64) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	mr.root = root
	mr.number = number

	if err := mr.db.Put(migratedRootKey, root.Bytes()); err != nil {
		return fmt.Errorf("failed to update migrated state root: %w", err)
	}
	if err := mr.db.Put(migratedNumberKey, []byte(hexutil.EncodeUint64(number))); err != nil {
		return fmt.Errorf("failed to update migrated block number: %w", err)
	}
	return nil
}

func (mr *MigratedRef) Root() common.Hash {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.root
}

func (mr *MigratedRef) BlockNumber() uint64 {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.number
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func AccountChangesKey(blockNumber uint64) []byte {
	return append(accountChangesPrefix, encodeBlockNumber(blockNumber)...)
}

func StorageChangesKey(blockNumber uint64) []byte {
	return append(storageChangesPrefix, encodeBlockNumber(blockNumber)...)
}

func DestructChangesKey(blockNumber uint64) []byte {
	return append(destructChangesPrefix, encodeBlockNumber(blockNumber)...)
}

func ReadStateChanges(db ethdb.KeyValueStore, blockNumber uint64) (map[common.Address]bool, map[common.Hash][]byte, map[common.Hash]map[common.Hash][]byte, error) {
	enc, err := db.Get(DestructChangesKey(blockNumber))
	if err != nil {
		return nil, nil, nil, err
	}
	destruct, err := DeserializeStateChanges[map[common.Address]bool](enc)
	if err != nil {
		return nil, nil, nil, err
	}

	enc, err = db.Get(AccountChangesKey(blockNumber))
	if err != nil {
		return nil, nil, nil, err
	}
	accounts, err := DeserializeStateChanges[map[common.Hash][]byte](enc)
	if err != nil {
		return nil, nil, nil, err
	}

	enc, err = db.Get(StorageChangesKey(blockNumber))
	if err != nil {
		return nil, nil, nil, err
	}
	storages, err := DeserializeStateChanges[map[common.Hash]map[common.Hash][]byte](enc)
	if err != nil {
		return nil, nil, nil, err
	}

	return destruct, accounts, storages, nil
}

// Note: it's should be called in MigrationTime
func WriteStateChanges(db ethdb.KeyValueStore, blockNumber uint64, stateObjectsDestruct map[common.Address]*types.StateAccount, accounts map[common.Hash][]byte, storages map[common.Hash]map[common.Hash][]byte) error {
	batch := db.NewBatch()

	stateObjectsDestructInfo := make(map[common.Address]bool)

	// TODO: Do we need to check if batch.ValueSize() > ethdb.IdealBatchSize, have storages size limit?
	for addr := range stateObjectsDestruct {
		if addr.Cmp(params.SystemAddress) == 0 {
			continue
		}
		stateObjectsDestructInfo[addr] = true
	}

	serializedDestruct, err := SerializeStateChanges(stateObjectsDestructInfo)
	if err != nil {
		return err
	}

	err = batch.Put(DestructChangesKey(blockNumber), serializedDestruct)
	if err != nil {
		return err
	}

	serializedAccounts, err := SerializeStateChanges(accounts)
	if err != nil {
		return err
	}

	err = batch.Put(AccountChangesKey(blockNumber), serializedAccounts)
	if err != nil {
		return err
	}

	serializedStorages, err := SerializeStateChanges(storages)
	if err != nil {
		return err
	}

	err = batch.Put(StorageChangesKey(blockNumber), serializedStorages)
	if err != nil {
		return err
	}

	if err := batch.Write(); err != nil {
		return err
	}
	batch.Reset()
	return nil
}

func DeleteStateChanges(db ethdb.KeyValueStore, blockNumber uint64) error {
	batch := db.NewBatch()
	defer batch.Reset()

	if err := batch.Delete(DestructChangesKey(blockNumber)); err != nil {
		return err
	}
	if err := batch.Delete(AccountChangesKey(blockNumber)); err != nil {
		return err
	}
	if err := batch.Delete(StorageChangesKey(blockNumber)); err != nil {
		return err
	}
	return batch.Write()
}

func SerializeStateChanges[T map[common.Address]bool | map[common.Hash][]byte | map[common.Hash]map[common.Hash][]byte](data T) ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := gob.NewEncoder(buf)
	if err := encoder.Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DeserializeStateChanges[T map[common.Address]bool | map[common.Hash][]byte | map[common.Hash]map[common.Hash][]byte](data []byte) (T, error) {
	var result T
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
