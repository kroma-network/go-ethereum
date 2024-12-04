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

package trie

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

func newEmptySecure() *StateTrie {
	trie, _ := NewStateTrie(TrieID(types.EmptyRootHash), NewDatabase(rawdb.NewMemoryDatabase(), nil))
	return trie
}

// makeTestStateTrie creates a large enough secure trie for testing.
func makeTestStateTrie() (*Database, *StateTrie, map[string][]byte) {
	// Create an empty trie
	triedb := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	trie, _ := NewStateTrie(TrieID(types.EmptyRootHash), triedb)

	// Fill it with some arbitrary data
	content := make(map[string][]byte)
	for i := byte(0); i < 255; i++ {
		// Map the same data under multiple keys
		key, val := common.LeftPadBytes([]byte{1, i}, 32), []byte{i}
		content[string(key)] = val
		trie.MustUpdate(key, val)

		key, val = common.LeftPadBytes([]byte{2, i}, 32), []byte{i}
		content[string(key)] = val
		trie.MustUpdate(key, val)

		// Add some other data to inflate the trie
		for j := byte(3); j < 13; j++ {
			key, val = common.LeftPadBytes([]byte{j, i}, 32), []byte{j, i}
			content[string(key)] = val
			trie.MustUpdate(key, val)
		}
	}
	root, nodes, _ := trie.Commit(false)
	if err := triedb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
		panic(fmt.Errorf("failed to commit db %v", err))
	}
	// Re-create the trie based on the new state
	trie, _ = NewStateTrie(TrieID(root), triedb)
	return triedb, trie, content
}

func TestSecureDelete(t *testing.T) {
	trie := newEmptySecure()
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"ether", ""},
		{"dog", "puppy"},
		{"shaman", ""},
	}
	for _, val := range vals {
		if val.v != "" {
			trie.MustUpdate([]byte(val.k), []byte(val.v))
		} else {
			trie.MustDelete([]byte(val.k))
		}
	}
	hash := trie.Hash()
	exp := common.HexToHash("29b235a58c3c25ab83010c327d5932bcf05324b7d6b1185e650798034783ca9d")
	if hash != exp {
		t.Errorf("expected %x got %x", exp, hash)
	}
}

func TestSecureGetKey(t *testing.T) {
	trie := newEmptySecure()
	trie.MustUpdate([]byte("foo"), []byte("bar"))

	key := []byte("foo")
	value := []byte("bar")
	seckey := crypto.Keccak256(key)

	if !bytes.Equal(trie.MustGet(key), value) {
		t.Errorf("Get did not return bar")
	}
	if k := trie.GetKey(seckey); !bytes.Equal(k, key) {
		t.Errorf("GetKey returned %q, want %q", k, key)
	}
}

func TestStateTrieConcurrency(t *testing.T) {
	// Create an initial trie and copy if for concurrent access
	_, trie, _ := makeTestStateTrie()

	threads := runtime.NumCPU()
	tries := make([]*StateTrie, threads)
	for i := 0; i < threads; i++ {
		tries[i] = trie.Copy()
	}
	// Start a batch of goroutines interacting with the trie
	pend := new(sync.WaitGroup)
	pend.Add(threads)
	for i := 0; i < threads; i++ {
		go func(index int) {
			defer pend.Done()

			for j := byte(0); j < 255; j++ {
				// Map the same data under multiple keys
				key, val := common.LeftPadBytes([]byte{byte(index), 1, j}, 32), []byte{j}
				tries[index].MustUpdate(key, val)

				key, val = common.LeftPadBytes([]byte{byte(index), 2, j}, 32), []byte{j}
				tries[index].MustUpdate(key, val)

				// Add some other data to inflate the trie
				for k := byte(3); k < 13; k++ {
					key, val = common.LeftPadBytes([]byte{byte(index), k, j}, 32), []byte{k, j}
					tries[index].MustUpdate(key, val)
				}
			}
			tries[index].Commit(false)
		}(i)
	}
	// Wait for all threads to finish
	pend.Wait()
}

// [Kroma: START]
func TestParsingAccountFromNodeSet(t *testing.T) {
	memdb := rawdb.NewMemoryDatabase()
	db := NewDatabase(memdb, &Config{
		Preimages: true,
		HashDB:    hashdb.Defaults,
	})
	id := StateTrieID(types.EmptyRootHash)
	mpt, err := NewStateTrie(id, db)
	require.NoError(t, err)

	updatedAccountsNum := 50000
	updatedAccounts := make(map[common.Address]*types.StateAccount)
	for i := 0; i < updatedAccountsNum; i++ {
		acc := types.NewEmptyStateAccount(false)
		ii := new(big.Int).SetUint64(uint64(i))
		acc.Balance = new(big.Int).Add(common.Big1, ii)
		addr := common.BigToAddress(acc.Balance)
		err = mpt.UpdateAccount(addr, acc)
		updatedAccounts[addr] = acc
		require.NoError(t, err)
	}

	newRoot, set, err := mpt.Commit(true)
	require.NoError(t, err)
	err = db.Update(newRoot, id.Root, 0, trienode.NewWithNodeSet(set), nil)
	require.NoError(t, err)
	err = db.Commit(newRoot, true)
	require.NoError(t, err)

	rootNode := set.Nodes[""]
	accounts := make(map[common.Address]*types.StateAccount)
	for path, node := range set.Nodes {
		if IsLeafNode(node.Blob) {
			fullPath, err := GetKeyFromPath(rootNode.Blob, memdb, []byte(path))
			require.NoError(t, err)
			addr := mpt.GetKey(fullPath)
			acc, err := NodeBlobToAccount(node.Blob)
			require.NoError(t, err)
			accounts[common.BytesToAddress(addr)] = acc
		}
	}

	require.Equal(t, updatedAccounts, accounts)
}

func TestParsingStorageFromNodeSet(t *testing.T) {
	memdb := rawdb.NewMemoryDatabase()
	db := NewDatabase(memdb, &Config{
		Preimages: true,
		HashDB:    hashdb.Defaults,
	})
	id := StateTrieID(types.EmptyRootHash)
	mpt, err := NewStateTrie(id, db)
	require.NoError(t, err)

	updatedStoragesNum := 50000
	updatedStorages := make(map[common.Hash][]byte)

	random := rand.New(rand.NewSource(time.Now().UnixNano()))

	acc := types.NewEmptyStateAccount(false)
	acc.Balance = big.NewInt(1)
	addr := common.BigToAddress(acc.Balance)
	err = mpt.UpdateAccount(addr, acc)
	storageId := StorageTrieID(id.Root, common.BytesToHash(zk.MustNewSecureHash(addr.Bytes()).Bytes()), acc.Root)
	storageMpt, err := NewStateTrie(storageId, db)
	require.NoError(t, err)

	for i := 0; i < updatedStoragesNum; i++ {
		// randomValue's minimum is 1
		randomValue := common.BigToHash(new(big.Int).Add(common.Big1, big.NewInt(random.Int63()))).Bytes()
		slot := big.NewInt(random.Int63()).Bytes()
		err = storageMpt.UpdateStorage(common.Address{}, slot, randomValue)
		updatedStorages[common.BytesToHash(slot)] = randomValue
		require.NoError(t, err)
	}

	newRoot, set, err := mpt.Commit(true)
	require.NoError(t, err)
	err = db.Update(newRoot, id.Root, 0, trienode.NewWithNodeSet(set), nil)
	require.NoError(t, err)
	err = db.Commit(newRoot, true)
	require.NoError(t, err)

	newStorageRoot, set, err := storageMpt.Commit(true)
	require.NoError(t, err)
	err = db.Update(newStorageRoot, storageId.Root, 0, trienode.NewWithNodeSet(set), nil)
	require.NoError(t, err)
	err = db.Commit(newStorageRoot, true)
	require.NoError(t, err)

	rootNode := set.Nodes[""]
	storages := make(map[common.Hash][]byte)
	for path, node := range set.Nodes {
		if IsLeafNode(node.Blob) {
			fullPath, err := GetKeyFromPath(rootNode.Blob, memdb, []byte(path))
			require.NoError(t, err)
			slot := mpt.GetKey(fullPath)
			slotValue, err := NodeBlobToSlot(node.Blob)

			require.NoError(t, err)
			storages[common.BytesToHash(slot)] = slotValue
		}
	}
	require.Equal(t, updatedStorages, storages)
}

// [Kroma: END]
