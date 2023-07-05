// Copyright 2018 The go-ethereum Authors
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

package downloader

import (
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

// Test chain parameters.
var (
	testZkKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testZkAddress = crypto.PubkeyToAddress(testZkKey.PublicKey)
	testZkDB      = rawdb.NewMemoryDatabase()

	testZkgspec = &core.Genesis{
		Config: func(config params.ChainConfig) *params.ChainConfig {
			config.Zktrie = true
			return &config
		}(*params.TestChainConfig),
		Alloc:   core.GenesisAlloc{testZkAddress: {Balance: big.NewInt(1000000000000000)}},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}
	testZkGenesis = testZkgspec.MustCommit(testZkDB, trie.NewDatabase(testZkDB, trie.ZkHashDefaults))
)

// The common prefix of all test chains:
var testZkChainBase *testChain

// Different forks on top of the base chain:
var testZkChainForkLightA, testZkChainForkLightB, testZkChainForkHeavy *testChain

var zkpregenerated bool

var (
	testZkBlockchains     = make(map[common.Hash]*testBlockchain)
	testZkBlockchainsLock sync.Mutex
)

func init() { initChains(true) }
