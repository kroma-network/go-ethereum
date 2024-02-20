// Copyright 2021 The go-ethereum Authors
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

package types

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const MintTokenTxType = 0x70

type MintTokenTx struct {
	From  common.Address  // From is always the mint function caller.
	To    *common.Address // To always points to the MinterContract address.
	Gas   uint64
	Nonce uint64
	Data  []byte
}

// copy creates a deep copy of the transaction data and initializes all fields.
func (tx *MintTokenTx) copy() TxData {
	cpy := &MintTokenTx{
		From:  tx.From,
		To:    copyAddressPtr(tx.To),
		Gas:   tx.Gas,
		Nonce: tx.Nonce,
		Data:  common.CopyBytes(tx.Data),
	}
	return cpy
}

// accessors for innerTx.
func (tx *MintTokenTx) txType() byte           { return MintTokenTxType }
func (tx *MintTokenTx) chainID() *big.Int      { return common.Big0 }
func (tx *MintTokenTx) accessList() AccessList { return nil }
func (tx *MintTokenTx) data() []byte           { return tx.Data }
func (tx *MintTokenTx) gas() uint64            { return tx.Gas }
func (tx *MintTokenTx) gasFeeCap() *big.Int    { return new(big.Int) }
func (tx *MintTokenTx) gasTipCap() *big.Int    { return new(big.Int) }
func (tx *MintTokenTx) gasPrice() *big.Int     { return new(big.Int) }
func (tx *MintTokenTx) value() *big.Int        { return common.Big0 }
func (tx *MintTokenTx) nonce() uint64          { return tx.Nonce }
func (tx *MintTokenTx) to() *common.Address    { return tx.To }

func (tx *MintTokenTx) effectiveGasPrice(dst *big.Int, _ *big.Int) *big.Int {
	return dst.Set(new(big.Int))
}

func (tx *MintTokenTx) effectiveNonce() *uint64 { return nil }

func (tx *MintTokenTx) rawSignatureValues() (v, r, s *big.Int) {
	return common.Big0, common.Big0, common.Big0
}

func (tx *MintTokenTx) setSignatureValues(_, _, _, _ *big.Int) {
	// noop
}

func (tx *MintTokenTx) encode(b *bytes.Buffer) error {
	return rlp.Encode(b, tx)
}

func (tx *MintTokenTx) decode(input []byte) error {
	return rlp.DecodeBytes(input, tx)
}
