package migration

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

func TestTrieMigration(t *testing.T) {
	memdb := rawdb.NewMemoryDatabase()
	mptdb := trie.NewDatabase(memdb, &trie.Config{
		Preimages: true,
		HashDB:    hashdb.Defaults,
	})

	id := trie.StateTrieID(types.EmptyRootHash)
	mpt, err := trie.NewStateTrie(id, mptdb)

	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		acc := types.NewEmptyStateAccount(false)
		ii := new(big.Int).SetUint64(uint64(i))
		acc.Balance = new(big.Int).Add(common.Big1, ii)
		err = mpt.UpdateAccount(common.BigToAddress(acc.Balance), acc)
		require.NoError(t, err)
	}

	root, set, err := mpt.Commit(true)
	require.NoError(t, err)

	err = mptdb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(set), nil)
	require.NoError(t, err)
	err = mptdb.Commit(root, true)
	require.NoError(t, err)

	id = trie.StateTrieID(root)
	tempMpt, err := trie.NewStateTrie(id, mptdb)
	leafcnt := 0
	totalcnt := 0
	rootNode := set.Nodes[""]

	accounts := make(map[common.Address]*types.StateAccount)
	// storages := make(map[common.Address]*types.StateAccount)
	for path, node := range set.Nodes {
		if trie.IsLeafNode(node.Blob) {
			leafcnt += 1
			fmt.Printf("leafnode path : %x\n", path)
			if node.IsDeleted() {
				fullPath := trie.GetFullPath(rootNode.Blob, memdb, []byte(path))
				addr := tempMpt.GetKey(fullPath)
				accounts[common.BytesToAddress(addr)] = nil
				fmt.Printf("fullpath %x\n", fullPath)
			} else {
				fullPath := trie.GetFullPath(rootNode.Blob, memdb, []byte(path))
				addr := tempMpt.GetKey(fullPath)
				acc, err := trie.NodeBlobToAccount(node.Blob)
				require.NoError(t, err)
				accounts[common.BytesToAddress(addr)] = acc
				fmt.Printf("fullpath %x\n", fullPath)
			}
		}
		totalcnt += 1
		_ = path
		_ = node
	}
	b, _ := json.MarshalIndent(accounts, "", "  ")

	fmt.Println(string(b))
}

/*
	TODO:
         - Get updated account addr & value
		 - Get deleted account addr
		 - Get updated storage slot
		 - Get deleted storage slot

    * 주의사항 : slot preimage와 addr preimage의 길이는 호출하는 쪽에서 맞게 조정해야한다.
*/
