package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleStackTrie struct {
	*zk.MerkleTree
	writeFn NodeWriteFunc
}

func NewZkStackTrie(writeFn NodeWriteFunc, nodeHasher zk.Hasher) *ZkMerkleStackTrie {
	if nodeHasher == nil {
		nodeHasher = zk.NewHasher()
	}
	return &ZkMerkleStackTrie{
		MerkleTree: zk.NewEmptyMerkleTree().WithMaxLevels(256).WithHasher(nodeHasher),
		writeFn:    writeFn,
	}
}

func (z *ZkMerkleStackTrie) Update(hash []byte, value []byte) error {
	return z.MerkleTree.Update(common.ReverseBytes(hash), value)
}

func (z *ZkMerkleStackTrie) Commit() (h common.Hash, err error) {
	var handleDirtyNode func(dirtyNode zk.TreeNode) error
	if z.writeFn != nil {
		handleDirtyNode = func(n zk.TreeNode) error {
			z.writeFn(zk.NewTreePathFromZkHash(*n.Hash()), common.BytesToHash(n.Hash().Bytes()), n.CanonicalValue())
			return nil
		}
	}
	if err = z.ComputeAllNodeHash(handleDirtyNode); err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(z.RootNode().Hash().Bytes()), nil
}

func (z *ZkMerkleStackTrie) Hash() common.Hash {
	root, _ := z.Commit()
	return root
}
