package trie

import (
	zktrie "github.com/kroma-network/zktrie/trie"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleTrie struct {
	*zk.MerkleTree
	db *Database
}

func NewZkMerkleTrie(merkleTree *zk.MerkleTree, db *Database) *ZkMerkleTrie {
	return &ZkMerkleTrie{MerkleTree: merkleTree, db: db}
}

func (z *ZkMerkleTrie) Hash() common.Hash {
	hash, _, _ := z.Commit(false)
	return hash
}

func (z *ZkMerkleTrie) MustNodeIterator(start []byte) NodeIterator {
	if it, err := z.NodeIterator(start); err != nil {
		log.Error("ZkMerkleTrie.MustNodeIterator", "error", err)
		return nil
	} else {
		return it
	}
}

func (z *ZkMerkleTrie) NodeIterator(startKey []byte) (NodeIterator, error) {
	nodeBlobFromTree, nodeBlobToIteratorNode := zkMerkleTreeNodeBlobFunctions(z.db.Get)
	return newMerkleTreeIterator(z.Hash(), nodeBlobFromTree, nodeBlobToIteratorNode, startKey), nil
}

func (z *ZkMerkleTrie) Commit(_ bool) (common.Hash, *trienode.NodeSet, error) {
	if root := z.RootNode().Hash(); root != nil {
		return common.BytesToHash(root.Bytes()), nil, nil
	}
	err := z.ComputeAllNodeHash(func(node zk.TreeNode) error { return z.db.Put(node.Hash()[:], node.CanonicalValue()) })
	if err != nil {
		log.Error("Failed to commit zk merkle trie", "err", err)
	}
	// Since NodeSet relies directly on mpt, we can't create a NodeSet.
	// Of course, we might be able to force-fit it by implementing the node interface.
	// However, NodeSet has been improved in geth, it could be improved to return a NodeSet when a commit is applied.
	// So let's not do that for the time being.
	// related geth commit : https://github.com/ethereum/go-ethereum/commit/bbcb5ea37b31c48a59f9aa5fad3fd22233c8a2ae
	return common.BytesToHash(z.RootNode().Hash().Bytes()), nil, nil
}

func (z *ZkMerkleTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	return z.prove(key, proofDb, func(node zk.TreeNode) error {
		return proofDb.Put(node.Hash()[:], node.CanonicalValue())
	})
}

func (z *ZkMerkleTrie) prove(key []byte, proofDb ethdb.KeyValueWriter, writeNode func(zk.TreeNode) error) error {
	if _, _, err := z.Commit(false); err != nil {
		return err
	} else if err := z.MerkleTree.Prove(key, writeNode); err != nil {
		return err
	}
	// we put this special kv pair in db so we can distinguish the type and make suitable Proof
	return proofDb.Put(magicHash, zktrie.ProofMagicBytes())
}
