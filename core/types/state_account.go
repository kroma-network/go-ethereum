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
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/iden3/go-iden3-crypto/utils"
)

//go:generate go run ../../rlp/rlpgen -type StateAccount -out gen_account_rlp.go

// StateAccount is the Ethereum consensus representation of accounts.
// These objects are stored in the main account trie.
type StateAccount struct {
	Nonce    uint64
	Balance  *big.Int
	Root     common.Hash // merkle root of the storage trie
	CodeHash []byte
}

// NewEmptyStateAccount constructs an empty state account.
func NewEmptyStateAccount(isZk bool) *StateAccount {
	return &StateAccount{
		Balance:  new(big.Int),
		Root:     GetEmptyRootHash(isZk),
		CodeHash: EmptyCodeHash.Bytes(),
	}
}

// NewStateAccount constructs an empty state account.
func NewStateAccount(data []byte, isZk bool) (*StateAccount, error) {
	if isZk {
		return UnmarshalStateAccount(data)
	}
	var account StateAccount
	if err := rlp.DecodeBytes(data, &account); err != nil {
		return nil, err // Returning the error here would drop the remote peer
	}
	return &account, nil
}

// Copy returns a deep-copied state account object.
func (acct *StateAccount) Copy() *StateAccount {
	var balance *big.Int
	if acct.Balance != nil {
		balance = new(big.Int).Set(acct.Balance)
	}
	return &StateAccount{
		Nonce:    acct.Nonce,
		Balance:  balance,
		Root:     acct.Root,
		CodeHash: common.CopyBytes(acct.CodeHash),
	}
}

func (acct *StateAccount) Encode(isZk bool) ([]byte, error) {
	if isZk {
		if !utils.CheckBigIntInField(acct.Balance) {
			return nil, errors.New("balance overflow")
		}
		result := make([]byte, 128)
		binary.BigEndian.PutUint64(result[24:32], acct.Nonce)
		acct.Balance.FillBytes(result[32:64])
		copy(result[64:96], acct.CodeHash)
		copy(result[96:128], acct.Root[:])
		return result, nil
	}
	return rlp.EncodeToBytes(acct)
}

// SlimAccount is a modified version of an Account, where the root is replaced
// with a byte slice. This format can be used to represent full-consensus format
// or slim format which replaces the empty root and code hash as nil byte slice.
type SlimAccount struct {
	Nonce    uint64
	Balance  *big.Int
	Root     []byte // Nil if root equals to types.EmptyRootHash
	CodeHash []byte // Nil if hash equals to types.EmptyCodeHash
}

// SlimAccountRLP encodes the state account in 'slim RLP' format.
func SlimAccountRLP(account StateAccount) []byte {
	slim := SlimAccount{
		Nonce:   account.Nonce,
		Balance: account.Balance,
	}
	if account.Root != EmptyRootHash {
		slim.Root = account.Root[:]
	}
	if !bytes.Equal(account.CodeHash, EmptyCodeHash[:]) {
		slim.CodeHash = account.CodeHash
	}
	data, err := rlp.EncodeToBytes(slim)
	if err != nil {
		panic(err)
	}
	return data
}

// FullAccount decodes the data on the 'slim RLP' format and returns
// the consensus format account.
func FullAccount(data []byte) (*StateAccount, error) {
	var slim SlimAccount
	if err := rlp.DecodeBytes(data, &slim); err != nil {
		return nil, err
	}
	var account StateAccount
	account.Nonce, account.Balance = slim.Nonce, slim.Balance

	// Interpret the storage root and code hash in slim format.
	if len(slim.Root) == 0 {
		account.Root = EmptyRootHash
	} else {
		account.Root = common.BytesToHash(slim.Root)
	}
	if len(slim.CodeHash) == 0 {
		account.CodeHash = EmptyCodeHash[:]
	} else {
		account.CodeHash = slim.CodeHash
	}
	return &account, nil
}

// FullAccountRLP converts data on the 'slim RLP' format into the full RLP-format.
func FullAccountRLP(data []byte) ([]byte, error) {
	account, err := FullAccount(data)
	if err != nil {
		return nil, err
	}
	return rlp.EncodeToBytes(account)
}

// -------------------------------
// Transformation function for ZK format

// SlimAccountZkBytes is zk version SlimAccountRLP
func SlimAccountZkBytes(account StateAccount) []byte {
	encoded, err := account.Encode(true)
	if err != nil {
		panic(err)
	}
	return encoded
}

// FullAccountZk is zk version FullAccount
func FullAccountZk(data []byte) (*StateAccount, error) {
	return UnmarshalStateAccount(data)
}

// FullAccountZkBytes is zk version FullAccountRLP
func FullAccountZkBytes(data []byte) ([]byte, error) {
	account, err := FullAccountZk(data)
	if err != nil {
		return nil, err
	}
	return account.Encode(true)
}

// -------------------------------
// Transformation functions. The processing method is determined by the 'isZk' parameter.

func SlimAccountBytes(account StateAccount, isZk bool) []byte {
	if isZk {
		return SlimAccountZkBytes(account)
	}
	return SlimAccountRLP(account)
}

func NewFullAccount(data []byte, isZk bool) (*StateAccount, error) {
	if isZk {
		return FullAccountZk(data)
	}
	return FullAccount(data)
}

func FullAccountBytes(data []byte, isZk bool) ([]byte, error) {
	if isZk {
		return FullAccountZkBytes(data)
	}
	return FullAccountRLP(data)
}

func NewSlimAccount(data []byte, isZk bool) (*SlimAccount, error) {
	if isZk {
		full, err := FullAccountZk(data)
		if err != nil {
			return nil, err
		}
		slim := &SlimAccount{
			Nonce:   full.Nonce,
			Balance: full.Balance,
		}
		if !isAllZero(full.CodeHash) {
			slim.CodeHash = full.CodeHash
		}
		if !isAllZero(full.Root[:]) {
			slim.Root = full.Root[:]
		}
		return slim, err
	}
	account := new(SlimAccount)
	return account, rlp.DecodeBytes(data, account)
}

func isAllZero(slice []byte) bool {
	for _, b := range slice {
		if b != 0 {
			return false
		}
	}
	return true
}
