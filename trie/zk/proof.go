package zk

import (
	"bytes"
	"fmt"

	"github.com/kroma-network/zktrie/trie"
	zkt "github.com/kroma-network/zktrie/types"
)

// Proof defines the required elements for a MT proof of existence or non-existence.
type Proof struct {
	// depth indicates how deep in the tree the proof goes.
	depth uint
	// notempties is a bitmap of non-empty Siblings found in Siblings.
	notempties [zkt.HashByteLen]byte
	// Siblings is a list of non-empty sibling node hashes.
	Siblings []*zkt.Hash
	// NodeAux contains the auxiliary information of the lowest common ancestor node in a non-existence proof.
	NodeAux *NodeAux

	Leaf *LeafNode
}

type NodeAux struct {
	Key   []byte    // Key is the node key
	Value *zkt.Hash // Value is the value hash in the node
}

func NewProof(
	rootHash *zkt.Hash,
	key []byte,
	findProofByHash func(key []byte) ([]byte, error),
	hasher Hasher,
) (*Proof, error) {
	path := NewTreePathFromBytes(key)
	hash := rootHash
	siblings, notempties := make([]*zkt.Hash, 0), [zkt.HashByteLen]byte{}
	for lvl := range path {
		if node, err := NewTreeNodeFromHash(hash, findProofByHash); err != nil {
			return nil, err
		} else {
			switch n := node.(type) {
			case *ParentNode:
				hash = n.Child(path.Get(lvl)).Hash()
				if sibling := n.Child(path.GetOther(lvl)); !bytes.Equal(sibling.Hash()[:], zkt.HashZero[:]) {
					zkt.SetBitBigEndian(notempties[:], uint(lvl))
					siblings = append(siblings, sibling.Hash())
				}
			case *LeafNode:
				if bytes.Equal(key, n.Key) {
					setNodeHash(n, hash)
					return &Proof{depth: uint(lvl), Siblings: siblings, notempties: notempties, Leaf: n}, nil
				}
				valueHash, err := computeLeafValueHash(hasher, n)
				if err != nil {
					return nil, err
				}
				return &Proof{depth: uint(lvl), Siblings: siblings, notempties: notempties, NodeAux: &NodeAux{Key: n.Key, Value: valueHash}}, nil
			case *EmptyNode, *HashNode:
				return &Proof{depth: uint(lvl), Siblings: siblings, notempties: notempties}, nil
			default:
				return nil, trie.ErrInvalidNodeFound
			}
		}
	}
	return nil, trie.ErrKeyNotFound
}

func (p *Proof) Existence() bool { return p.Leaf != nil }

func VerifyProof(
	root *zkt.Hash,
	key []byte,
	findProofByHash func(key []byte) ([]byte, error),
	hasher Hasher,
) (*LeafNode, error) {
	proof, err := NewProof(root, key, findProofByHash, hasher)
	if err != nil || !proof.Existence() {
		return nil, err
	}
	if err := verifyProof(root, proof, hasher); err != nil {
		return nil, err
	}
	return proof.Leaf, nil
}

func verifyProof(root *zkt.Hash, proof *Proof, hasher Hasher) error {
	if rootFromProof, err := rootHashFromProof(proof, hasher); err != nil {
		return err
	} else if !bytes.Equal(root[:], rootFromProof[:]) {
		return fmt.Errorf("bad proof node %v", proof)
	}
	return nil
}

func rootHashFromProof(proof *Proof, hasher Hasher) (*zkt.Hash, error) {
	path := NewTreePathFromBytes(proof.Leaf.Key)
	var node TreeNode = proof.Leaf
	for sibIdx, lvl := len(proof.Siblings)-1, int(proof.depth)-1; lvl >= 0; lvl-- {
		if zkt.TestBitBigEndian(proof.notempties[:], uint(lvl)) {
			node = newParentNode(path.Get(lvl), node, path.GetOther(lvl), NewHashNode(proof.Siblings[sibIdx]))
			sibIdx--
		} else {
			node = newParentNode(path.Get(lvl), node, path.GetOther(lvl), EmptyNodeValue)
		}
	}
	if err := ComputeNodeHash(hasher, node, nil); err != nil {
		return nil, err
	}
	return node.Hash(), nil
}
