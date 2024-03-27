package zk

import (
	"errors"
	"math/big"

	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
)

// MarshalBytes transforms data into a format acceptable to LeafNode.
// The rule referred to https://github.com/kroma-network/zktrie/blob/b0f3ee937287a115ea44f0c188df27e4cd29dfa0/lib.go#L224
func MarshalBytes(data []byte) (compressedFlag uint32, values []zkt.Byte32, err error) {
	if len(data) <= 32 {
		return 1, []zkt.Byte32{*zkt.NewByte32FromBytes(data)}, nil
	}
	switch len(data) {
	case 128:
		return 4, []zkt.Byte32{
			*zkt.NewByte32FromBytes(data[0:32]),
			*zkt.NewByte32FromBytes(data[32:64]),
			*zkt.NewByte32FromBytes(data[64:96]),
			*zkt.NewByte32FromBytes(data[96:128]),
		}, nil
	case 160:
		return 8, []zkt.Byte32{
			*zkt.NewByte32FromBytes(data[0:32]),
			*zkt.NewByte32FromBytes(data[32:64]),
			*zkt.NewByte32FromBytes(data[64:96]),
			*zkt.NewByte32FromBytes(data[96:128]),
			*zkt.NewByte32FromBytes(data[128:160]),
		}, nil
	default:
		return 0, nil, errors.New("unexpected buffer type")
	}
}

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

func copyNode(n TreeNode) TreeNode {
	switch node := n.(type) {
	case *ParentNode:
		return &ParentNode{childL: copyNode(node.childL), childR: copyNode(node.childR), hash: node.hash}
	}
	return n
}

func clearNodeHash(n TreeNode) {
	switch node := n.(type) {
	case *ParentNode:
		node.hash = nil
	case *LeafNode:
		node.hash = nil
	}
}

func setNodeHash(n TreeNode, hash *zkt.Hash) {
	switch node := n.(type) {
	case *ParentNode:
		node.hash = hash
	case *LeafNode:
		node.hash = hash
	}
}

func computeNodeHash(n TreeNode, handleDirtyNode func(dirtyNode TreeNode) error) (err error) {
	if n.Hash() != nil {
		return nil
	}
	switch node := n.(type) {
	case *ParentNode:
		for _, child := range node.Children() {
			if err = computeNodeHash(child, handleDirtyNode); err != nil {
				return err
			}
		}
		node.hash, err = zkt.HashElems(node.ChildL().Hash().BigInt(), node.ChildR().Hash().BigInt())
		if err == nil && handleDirtyNode != nil {
			err = handleDirtyNode(node)
		}
	case *LeafNode:
		var valueHash *zkt.Hash
		if valueHash, err = zkt.PreHandlingElems(node.CompressedFlags, node.ValuePreimage); err == nil {
			node.hash, err = zkt.HashElems(common.Big1, new(big.Int).SetBytes(common.ReverseBytes(node.Key)), valueHash.BigInt())
		}
		if err == nil && handleDirtyNode != nil {
			err = handleDirtyNode(node)
		}
	}
	return err
}

func min(x, y, z int) int {
	min := x
	if min > y {
		min = y
	}
	if min > z {
		min = z
	}
	return min
}
