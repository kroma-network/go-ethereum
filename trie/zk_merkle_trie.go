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
	db                *Database
	logger            log.Logger
	transformKey      func(key []byte) ([]byte, error)
	transformProveKey func(key []byte) []byte
}

func (z *ZkMerkleTrie) WithTransformKey(transformKey func(key []byte) ([]byte, error)) *ZkMerkleTrie {
	z.transformKey = transformKey
	return z
}

func (z *ZkMerkleTrie) WithTransformProveKey(transformProveKey func(key []byte) []byte) *ZkMerkleTrie {
	z.transformProveKey = transformProveKey
	return z
}

func NewZkMerkleTrie(merkleTree *zk.MerkleTree, db *Database) *ZkMerkleTrie {
	return &ZkMerkleTrie{
		MerkleTree:        merkleTree,
		db:                db,
		logger:            log.New("trie", "ZkMerkleTrie"),
		transformKey:      func(key []byte) ([]byte, error) { return common.ReverseBytes(key), nil },
		transformProveKey: func(key []byte) []byte { return common.ReverseBytes(key) },
	}
}

func (z *ZkMerkleTrie) GetNode(compactPath []byte) ([]byte, int, error) {
	node := z.MerkleTree.GetNodeByPath(compactToHex(compactPath))
	return node.CanonicalValue(), 0, nil
}

func (z *ZkMerkleTrie) MustGet(key []byte) []byte {
	if data, err := z.Get(key); err != nil {
		z.logger.Error("failed to MustGet", "error", err, "key", key)
		return nil
	} else {
		return data
	}
}

func (z *ZkMerkleTrie) Get(key []byte) ([]byte, error) {
	if key, err := z.transformKey(key); err != nil {
		return nil, err
	} else {
		return z.MerkleTree.Get(key)
	}
}

func (z *ZkMerkleTrie) GetLeafNode(key []byte) (*zk.LeafNode, error) {
	if key, err := z.transformKey(key); err != nil {
		return nil, err
	} else {
		return z.MerkleTree.GetLeafNode(key)
	}
}

func (z *ZkMerkleTrie) MustUpdate(key, value []byte) {
	if err := z.Update(key, value); err != nil {
		z.logger.Error("failed to MustUpdate", "error", err, "key", key, "value", value)
	}
}

func (z *ZkMerkleTrie) Update(key, value []byte) error {
	if key, err := z.transformKey(key); err != nil {
		return err
	} else {
		return z.MerkleTree.Update(key, value)
	}
}

func (z *ZkMerkleTrie) MustDelete(key []byte) {
	if err := z.Delete(key); err != nil {
		z.logger.Error("failed to MustDelete", "error", err, "key", key)
	}
}

func (z *ZkMerkleTrie) Delete(key []byte) error {
	if key, err := z.transformKey(key); err != nil {
		return err
	} else {
		return z.MerkleTree.Delete(key)
	}
}

func (z *ZkMerkleTrie) Hash() common.Hash {
	hash, _, _ := z.Commit(false)
	return hash
}

func (z *ZkMerkleTrie) MustNodeIterator(start []byte) NodeIterator {
	if it, err := z.NodeIterator(start); err != nil {
		z.logger.Error("failed to MustNodeIterator", "error", err, "start", start)
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
		z.logger.Error("failed to Commit", "error", err)
		return common.Hash{}, nil, err
	}
	// Since NodeSet relies directly on mpt, we can't create a NodeSet.
	// Of course, we might be able to force-fit it by implementing the node interface.
	// However, NodeSet has been improved in geth, it could be improved to return a NodeSet when a commit is applied.
	// So let's not do that for the time being.
	// related geth commit : https://github.com/ethereum/go-ethereum/commit/bbcb5ea37b31c48a59f9aa5fad3fd22233c8a2ae
	return common.BytesToHash(z.RootNode().Hash().Bytes()), nil, nil
}

func (z *ZkMerkleTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	return z.prove(z.transformProveKey(key), proofDb, func(node zk.TreeNode) error {
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

func (z *ZkMerkleTrie) Copy() *ZkMerkleTrie {
	return &ZkMerkleTrie{z.MerkleTree.Copy(), z.db, z.logger, z.transformKey, z.transformProveKey}
}
