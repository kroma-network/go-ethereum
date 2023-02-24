package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

type LowLevelTrie interface {
	Hash() common.Hash
	TryGetNode(path []byte) ([]byte, int, error)
	Get(key []byte) []byte
	Update(key, value []byte)
	NodeIterator(startKey []byte) NodeIterator
	Commit(collectLeaf bool) (common.Hash, *NodeSet, error)
	Prove(key []byte, fromLevel uint, proofDb ethdb.KeyValueWriter) error
}
