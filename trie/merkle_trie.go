package trie

import (
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

/*
There are quite a few pieces of code that use trie as a structure rather than an interface.
The purpose of this file is to define an interface to make TRIE compatible with ZK.
They are compatible as follows

   geth      |  scroll   |    kroma
   ----------------------------------------
   Trie      |           |  ZkMerkleTrie
   StackTrie |           |  ZkMerkleStackTrie
   StateTrie |  ZkTrie   |  ZkMerkleStateTrie
*/

// MerkleTrie is a data structure that does not hash the key.
// The main purpose is to make Trie and zk.MerkleTree compatible.
// ZkTrie is not compatible with MerkleTrie because it always hashes keys.
type MerkleTrie interface {
	Hash() common.Hash
	Get(key []byte) ([]byte, error)
	MustUpdate(key, value []byte)
	Update(key, value []byte) error
	MustNodeIterator(start []byte) NodeIterator
	NodeIterator(startKey []byte) (NodeIterator, error)
	Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error)
	Prove(key []byte, proofDb ethdb.KeyValueWriter) error
}

func NewMerkleTrie(id *ID, db *Database) (MerkleTrie, error) {
	if db.IsZk() {
		tree, err := zk.NewMerkleTreeFromHash(zkt.NewHashFromBytes(id.Root[:]), db.Get)
		if err != nil {
			return nil, err
		}
		return NewZkMerkleTrie(tree, db), nil
	}
	return New(id, db)
}

func NewEmptyMerkleTrie(db *Database) MerkleTrie {
	if db.IsZk() {
		return NewZkMerkleTrie(zk.NewEmptyMerkleTree(), db)
	}
	return NewEmpty(db)
}

type MerkleStackTrie interface {
	Update([]byte, []byte) error
	Commit() (h common.Hash, err error)
	Hash() common.Hash
}

func NewMerkleStackTrie(writeFn NodeWriteFunc, isZk bool) MerkleStackTrie {
	if isZk {
		return NewZkStackTrie(writeFn)
	}
	return NewStackTrie(writeFn)
}

// MerkleStateTrie Interface to make StateTrie and ZkTrie and ZkMerkleStateTrie compatible.
type MerkleStateTrie interface {
	Hash() common.Hash
	MustGet(key []byte) []byte
	UpdateAccount(address common.Address, account *types.StateAccount) error
	MustUpdate(key, value []byte)
	MustDelete(key []byte)
	MustNodeIterator(start []byte) NodeIterator
	Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error)
	Prove(key []byte, proofDb ethdb.KeyValueWriter) error
}

func NewMerkleStateTrie(id *ID, db *Database) (MerkleStateTrie, error) {
	if db.IsKromaZK() {
		return NewZkMerkleStateTrie(id.Root, db)
	}
	if db.IsZk() {
		return NewZkTrie(id.Root, db)
	}
	return NewStateTrie(id, db)
}
