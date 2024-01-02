package trie

import (
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie/zk"
)

// File for defining an iterator key and managing key <-> iterator key conversion functions.
//
// NodeIterator.LeafKey must satisfy the following characteristics
// ```
// preordertraversal(tree).filter(x -> x is leaf).map(x -> x.leafKey) == sort(leafKeys)
// ```
// Since Trie satisfies this condition, functions that utilize it do not require any additional code (e.g. snapsync).
// However, zktrie does not, so unless we introduce a new key concept, we need to modify the core source code quite a bit.
// Therefore, we introduced the concept of iteratorKey, which satisfies the above condition, and implemented functions to convert key and iteratorKey.

func HashToIteratorKey(hash common.Hash, isZk bool) common.Hash {
	if isZk {
		return HashToZkIteratorKey(hash)
	}
	return hash
}

func HashToZkIteratorKey(hash common.Hash) common.Hash {
	return common.BigToHash(zk.NewTreePathFromHash(hash).ToBigInt())
}

func BytesToZkIteratorKey(data []byte) common.Hash {
	return common.BigToHash(zk.NewTreePathFromBytes(data).ToBigInt())
}

func IteratorKeyToHash(key []byte, isZk bool) *common.Hash {
	if isZk {
		return zk.NewTreePathFromHashBig(common.BytesToHash(key)).ToHash()
	}
	hash := common.BytesToHash(key)
	return &hash
}

func ZkIteratorKeyToHash(key common.Hash) common.Hash {
	return *zk.NewTreePathFromHashBig(key).ToHash()
}

func ZkIteratorKeyToZkHash(key common.Hash) *zkt.Hash {
	return zk.NewTreePathFromHashBig(key).ToZkHash()
}
