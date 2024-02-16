package zk

import (
	"bytes"
	"encoding/binary"
	"unsafe"

	"github.com/kroma-network/zktrie/trie"
	zkt "github.com/kroma-network/zktrie/types"
)

const NodeTypeHash trie.NodeType = 9

type TreeNode interface {
	Hash() *zkt.Hash

	// CanonicalValue returns the byte form of a node required to be persisted, and strip unnecessary fields
	// from the encoding (current only KeyPreimage for Leaf node) to keep a minimum size for content being
	// stored in backend storage
	CanonicalValue() []byte
}

func NewTreeNodeFromHash(
	hash *zkt.Hash,
	findBlobByHash func(key []byte) ([]byte, error),
) (TreeNode, error) {
	if blob, err := findBlobByHash(hash[:]); err != nil {
		return nil, err
	} else if node, err := NewTreeNodeFromBlob(blob); err != nil {
		return nil, err
	} else {
		setNodeHash(node, hash)
		return node, nil
	}
}

func NewTreeNodeFromBlob(b []byte) (TreeNode, error) {
	node, _, err := NewTreeNodeWithKeyPreimageFromBlob(b)
	return node, err
}

func NewTreeNodeWithKeyPreimageFromBlob(b []byte) (node TreeNode, keyPreimage *zkt.Byte32, err error) {
	if len(b) == 0 {
		return nil, nil, trie.ErrNodeBytesBadSize
	}
	switch trie.NodeType(b[0]) {
	case trie.NodeTypeParent:
		node, err = newParentNodeFromBlob(b[1:])
		return node, nil, err
	case trie.NodeTypeLeaf:
		return newLeafNodeFromBlob(b[1:])
	case trie.NodeTypeEmpty:
		return EmptyNodeValue, nil, nil
	case NodeTypeHash:
		return NewHashNode(zkt.NewHashFromBytes(b[1:])), nil, nil
	default:
		return nil, nil, trie.ErrInvalidNodeFound
	}
}

type ParentNode struct {
	childL TreeNode
	childR TreeNode
	hash   *zkt.Hash
}

func newParentNode(child1Path byte, child1 TreeNode, child2Path byte, child2 TreeNode) *ParentNode {
	n := &ParentNode{}
	n.SetChild(child1Path, child1)
	n.SetChild(child2Path, child2)
	return n
}

func newParentNodeFromBlob(blob []byte) (*ParentNode, error) {
	if len(blob) != 2*zkt.HashByteLen {
		return nil, trie.ErrNodeBytesBadSize
	}
	return &ParentNode{
		NewHashNode(zkt.NewHashFromBytes(blob[:zkt.HashByteLen])),
		NewHashNode(zkt.NewHashFromBytes(blob[zkt.HashByteLen : 2*zkt.HashByteLen])),
		nil,
	}, nil
}

func (n *ParentNode) Hash() *zkt.Hash { return n.hash }

func (n *ParentNode) CanonicalValue() []byte {
	values := []byte{byte(trie.NodeTypeParent)}
	values = append(values, n.childL.Hash().Bytes()...)
	values = append(values, n.childR.Hash().Bytes()...)
	return values
}

func (n *ParentNode) Child(path byte) TreeNode {
	if path == right {
		return n.childR
	}
	return n.childL
}

// SetChild Sets the child and clears the hash.
func (n *ParentNode) SetChild(path byte, child TreeNode) {
	oldChild := n.Child(path)
	if path == right {
		n.childR = child
	} else {
		n.childL = child
	}
	if _, ok := oldChild.(*HashNode); ok && child.Hash() != nil && bytes.Equal(oldChild.Hash()[:], child.Hash()[:]) {
		// This is a case of converting a HashNode to the original TreeNode. Does not clear the hash.
		return
	}
	n.hash = nil
}

func (n *ParentNode) ChildL() TreeNode      { return n.childL }
func (n *ParentNode) ChildR() TreeNode      { return n.childR }
func (n *ParentNode) Children() [2]TreeNode { return [2]TreeNode{n.childL, n.childR} }

type LeafNode struct {
	Key []byte

	// ValuePreimage can store at most 256 byte32 as fields (represented by BIG-ENDIAN integer)
	// and the first 24 can be compressed (each bytes32 consider as 2 fields), in hashing the compressed
	// elements would be calculated first
	ValuePreimage []zkt.Byte32

	// CompressedFlags use each bit for indicating the compressed flag for the first 24 fields
	CompressedFlags uint32

	hash *zkt.Hash
}

func NewLeafNode(key []byte, value []byte) (*LeafNode, error) {
	if flag, values, err := MarshalBytes(value); err == nil {
		return &LeafNode{Key: key, CompressedFlags: flag, ValuePreimage: values}, nil
	} else {
		return nil, err
	}
}

// blob format is described in LeafNode.CanonicalValue
func newLeafNodeFromBlob(blob []byte) (*LeafNode, *zkt.Byte32, error) {
	if len(blob) < zkt.HashByteLen+4 {
		return nil, nil, trie.ErrNodeBytesBadSize
	}
	hashBlob := blob[:zkt.HashByteLen]
	mark := binary.LittleEndian.Uint32(blob[zkt.HashByteLen : zkt.HashByteLen+4])
	blob = blob[zkt.HashByteLen+4:]
	node := &LeafNode{
		Key:             zkt.ReverseByteOrder(hashBlob),
		CompressedFlags: mark >> 8,
		ValuePreimage:   make([]zkt.Byte32, int(mark&255)),
	}
	if len(blob) < len(node.ValuePreimage)*32+1 {
		return nil, nil, trie.ErrNodeBytesBadSize
	}
	for i := 0; i < len(node.ValuePreimage); i++ {
		copy(node.ValuePreimage[i][:], blob[i*32:(i+1)*32])
	}
	blob = blob[len(node.ValuePreimage)*32:]
	if preImageSize := int(blob[0]); preImageSize != 0 {
		if len(blob) < 1+preImageSize {
			return nil, nil, trie.ErrNodeBytesBadSize
		}
		keyPreimage := new(zkt.Byte32)
		copy(keyPreimage[:], blob[1:1+preImageSize])
		return node, keyPreimage, nil
	}
	return node, nil, nil
}

func (n *LeafNode) Hash() *zkt.Hash { return n.hash }

// CanonicalValue [type, Key[0], ..., Key[31], mark[0], ..., mark[3], Data[0], ..., Data[32*len(ValuePreimage)], key preimage len]
// Leaf node does not provide key preimage, so set the length to 0.
// If you can provide the key preimage, change the last 0 and add the key preimage afterwards. See ZkMerkleStateTrie.Prove
func (n *LeafNode) CanonicalValue() []byte {
	key := make([]byte, 32)
	copy(key[:], zkt.ReverseByteOrder(n.Key))
	var buf bytes.Buffer
	buf.WriteByte(byte(trie.NodeTypeLeaf))
	buf.Write(key)
	binary.Write(&buf, binary.LittleEndian, (n.CompressedFlags<<8)+uint32(len(n.ValuePreimage)))
	buf.Write(n.Data())
	buf.WriteByte(0) // key preimage len
	return buf.Bytes()
}

func (n *LeafNode) Data() []byte {
	ptr := *(*[]byte)(unsafe.Pointer(&n.ValuePreimage))
	return unsafe.Slice(unsafe.SliceData(ptr), 32*len(n.ValuePreimage))
}

type HashNode zkt.Hash

func NewHashNode(hash *zkt.Hash) TreeNode {
	if bytes.Equal(hash[:], zkt.HashZero[:]) {
		return EmptyNodeValue
	}
	node := HashNode(*hash)
	return &node
}

func (n *HashNode) Hash() *zkt.Hash {
	hash := zkt.Hash(*n)
	return &hash
}

func (n *HashNode) CanonicalValue() []byte {
	return append([]byte{byte(NodeTypeHash)}, n.Hash().Bytes()...)
}

type EmptyNode struct{}

func (EmptyNode) Hash() *zkt.Hash        { return &zkt.HashZero }
func (EmptyNode) CanonicalValue() []byte { return []byte{byte(trie.NodeTypeEmpty)} }

var EmptyNodeValue = &EmptyNode{}
