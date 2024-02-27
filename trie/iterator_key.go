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

func BytesToZkIteratorKey(data []byte) common.Hash {
	return common.BigToHash(zk.NewTreePathFromBytes(data).ToBigInt())
}

func ZkIteratorKeyToZkHash(key common.Hash) zkt.Hash {
	return *zk.NewTreePathFromHashBig(key).ToZkHash()
}
