package trie

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie/testutil"
	"github.com/ethereum/go-ethereum/trie/zk"
)

func TestZkTrieNoExistKey(t *testing.T) {
	testMerkleStateTrieNoExistKey(t, newEmptyZkTrie())
	testMerkleStateTrieNoExistKey(t, NewEmptyZkMerkleStateTrie(NewZkDatabase(rawdb.NewMemoryDatabase())))
}

func testMerkleStateTrieNoExistKey(t *testing.T, trie MerkleStateTrie) {
	address := common.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")
	if acc, err := trie.GetAccount(address); acc != nil || err != nil {
		t.Errorf("GetAccount return acc : %v, err : %v", acc, err)
	}
	assert.NoError(t, trie.DeleteAccount(address))

	storageKey := common.LeftPadBytes([]byte{1}, 20)
	if val, err := trie.GetStorage(address, storageKey); len(val) > 0 || err != nil {
		t.Errorf("GetStorage return acc : %v, err : %v", val, err)
	}
	assert.NoError(t, trie.DeleteStorage(address, storageKey))
}

func TestProve(t *testing.T) {
	t.Run("without preimage", func(t *testing.T) {
		scrolldb, kromadb := NewZkDatabase(rawdb.NewMemoryDatabase()), NewZkDatabase(rawdb.NewMemoryDatabase())
		root, keys := prepareTrie(t, scrolldb, kromadb)
		assertProof(t, must(NewZkTrie(root, scrolldb)), must(NewZkMerkleStateTrie(root, kromadb)), keys)
	})

	t.Run("with preimage", func(t *testing.T) {
		config := ZkHashDefaults
		config.Preimages = true
		scrolldb, kromadb := NewDatabase(rawdb.NewMemoryDatabase(), config), NewDatabase(rawdb.NewMemoryDatabase(), config)
		root, keys := prepareTrie(t, scrolldb, kromadb)
		assertProof(t, must(NewZkTrie(root, scrolldb)), must(NewZkMerkleStateTrie(root, kromadb)), keys)
	})
}

func assertProof(t *testing.T, scrollTrie *ZkTrie, kromaTrie *ZkMerkleStateTrie, keys []common.Hash) {
	for _, hash := range keys {
		scrollDB := rawdb.NewMemoryDatabase()
		assert.NoError(t, scrollTrie.Prove(hash.Bytes(), scrollDB))

		kromaDB := rawdb.NewMemoryDatabase()
		assert.NoError(t, kromaTrie.Prove(hash.Bytes(), kromaDB))

		for scrollIt, kromaIt := scrollDB.NewIterator(nil, nil), kromaDB.NewIterator(nil, nil); ; {
			scrollNext, kromaNext := scrollIt.Next(), kromaIt.Next()
			assert.Equal(t, scrollNext, kromaNext)
			assert.Equal(t, scrollIt.Value(), kromaIt.Value())
			if !scrollNext {
				break
			}
		}
	}
}

func prepareTrie(t *testing.T, scrollDB *Database, kromaDB *Database) (rootHash common.Hash, keys []common.Hash) {
	scrollTrie, _ := NewZkTrie(common.Hash{}, scrollDB)
	kromaTrie := NewEmptyZkMerkleStateTrie(kromaDB)

	keyHashes := make([]common.Hash, 100)
	for i := 0; i < len(keyHashes); i++ {
		address, account := newRandomStateAccount()
		keyHashes[i] = common.BytesToHash(zk.MustNewSecureHash(address.Bytes()).Bytes())
		assert.NoError(t, scrollTrie.UpdateAccount(address, account))
		assert.NoError(t, kromaTrie.UpdateAccount(address, account))
	}
	scrollRoot, _, _ := scrollTrie.Commit(false)
	kromaRoot, _, _ := kromaTrie.Commit(false)
	assert.Equal(t, scrollRoot, kromaRoot)
	assert.NoError(t, scrollDB.Commit(scrollRoot, false))
	assert.NoError(t, kromaDB.Commit(kromaRoot, false))
	return scrollRoot, keyHashes
}

func newRandomStateAccount() (common.Address, *types.StateAccount) {
	return testutil.RandomAddress(), &types.StateAccount{
		Nonce:    uint64(rand.Uint32()),
		Balance:  big.NewInt(int64(rand.Uint32())),
		Root:     common.BigToHash(big.NewInt(int64(rand.Uint32()))),
		CodeHash: common.BigToHash(big.NewInt(int64(rand.Uint32()))).Bytes(),
	}
}

func must[R any](r R, err error) R {
	if err != nil {
		panic(err)
	}
	return r
}
