package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleStackTrie struct {
	*zk.MerkleTree
	options *StackTrieOptions
}

func NewZkStackTrie(options *StackTrieOptions) *ZkMerkleStackTrie {
	if options.zkNodeHasher == nil {
		options.zkNodeHasher = zk.NewHasher()
	}
	return &ZkMerkleStackTrie{
		MerkleTree: zk.NewEmptyMerkleTree().WithMaxLevels(256).WithHasher(options.zkNodeHasher),
		options:    options,
	}
}

func (z *ZkMerkleStackTrie) Update(hash []byte, value []byte) error {
	return z.MerkleTree.Update(common.ReverseBytes(hash), value)
}

func (z *ZkMerkleStackTrie) Commit() common.Hash {
	var handleDirtyNode func(dirtyNode zk.TreeNode) error
	if z.options.Writer != nil {
		handleDirtyNode = func(n zk.TreeNode) error {
			z.options.Writer(zk.NewTreePathFromZkHash(*n.Hash()), common.BytesToHash(n.Hash().Bytes()), n.CanonicalValue())
			return nil
		}
	}
	if err := z.ComputeAllNodeHash(handleDirtyNode); err != nil {
		log.Error("failed to ZkMerkleStackTrie.ComputeAllNodeHash", err)
	}
	return common.BytesToHash(z.RootNode().Hash().Bytes())
}

func (z *ZkMerkleStackTrie) Hash() common.Hash {
	return z.Commit()
}
