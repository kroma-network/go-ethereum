package zk

import (
	"math/big"

	zkt "github.com/kroma-network/zktrie/types"
)

var bigOne = big.NewInt(1)

func NewSecureHash(b []byte) (*zkt.Hash, error) {
	k, err := zkt.ToSecureKey(b)
	if err != nil {
		return nil, err
	}
	return zkt.NewHashFromBigInt(k), nil
}

func MustNewSecureHash(b []byte) *zkt.Hash {
	hash, err := NewSecureHash(b)
	if err != nil {
		panic(err)
	}
	return hash
}

func clearNodeHash(n TreeNode) {
	switch node := n.(type) {
	case *ParentNode:
		node.hash = nil
	case *LeafNode:
		node.ValueHash = nil
		node.hash = nil
	case *EmptyNode:
	case *HashNode:
	}
}

func setNodeHash(n TreeNode, hash *zkt.Hash) {
	switch node := n.(type) {
	case *ParentNode:
		node.hash = hash
	case *LeafNode:
		node.hash = hash
	case *EmptyNode:
	case *HashNode:
	}
}

func computeNodeHash(n TreeNode, handleDirtyNode func(dirtyNode TreeNode) error) (err error) {
	switch node := n.(type) {
	case *ParentNode:
		if node.hash == nil {
			for _, child := range node.Children() {
				if err = computeNodeHash(child, handleDirtyNode); err != nil {
					return err
				}
			}
			node.hash, err = zkt.HashElems(node.ChildL().Hash().BigInt(), node.ChildR().Hash().BigInt())
			if err == nil && handleDirtyNode != nil {
				err = handleDirtyNode(node)
			}
		}
	case *LeafNode:
		if node.ValueHash == nil {
			node.ValueHash, err = zkt.PreHandlingElems(node.CompressedFlags, node.ValuePreimage)
		}
		if node.hash == nil && err == nil {
			node.hash, err = zkt.HashElems(bigOne, node.KeyHash.BigInt(), node.ValueHash.BigInt())
			if err == nil && handleDirtyNode != nil {
				err = handleDirtyNode(node)
			}
		}
	case *EmptyNode:
	case *HashNode:
	}
	return
}
