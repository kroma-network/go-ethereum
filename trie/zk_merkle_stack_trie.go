package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleStackTrie struct {
	*zk.MerkleTree
	writeFn NodeWriteFunc
	owner   common.Hash
}

func NewZkStackTrie(writeFn NodeWriteFunc, owner common.Hash) *ZkMerkleStackTrie {
	return &ZkMerkleStackTrie{MerkleTree: zk.NewEmptyMerkleTree(), writeFn: writeFn, owner: owner}
}

func (z *ZkMerkleStackTrie) Update(hash []byte, value []byte) error {
	return z.MerkleTree.Update(hash, value)
}

func (z *ZkMerkleStackTrie) Commit() (h common.Hash, err error) {
	var handleDirtyNode func(dirtyNode zk.TreeNode) error
	if z.writeFn != nil {
		handleDirtyNode = func(n zk.TreeNode) error {
			z.writeFn(z.owner, zk.NewTreePathFromZkHash(*n.Hash()), common.BytesToHash(n.Hash().Bytes()), n.CanonicalValue())
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
