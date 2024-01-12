// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"bytes"
	"errors"
	"fmt"

	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/zk"
)

// VerifyRangeProofZk is similar to VerifyRangeProof, but it verifies the zk tree.
// Most of the conditions or logic is similar to VerifyRangeProof
func VerifyRangeProofZk(
	rootHash common.Hash,
	firstKey []byte,
	keys [][]byte,
	values [][]byte,
	proof ethdb.KeyValueReader,
	configTree func(tree *zk.MerkleTree),
) (bool, error) {
	if err := validateKeyValue(keys, values); err != nil {
		return false, err
	}
	if proof == nil { // Special case, there is no edge proof at all. The given range is expected to be the whole leaf-set in the trie.
		return false, verifyRootHash(rootHash, zk.NewEmptyMerkleTree(), keys, values, configTree)
	}
	root, err := resolveRootNode(rootHash, proof)
	if err != nil {
		return false, err
	}
	// Special case, there is a provided edge proof but zero key/value pairs, ensure there are no more accounts / slots in the trie.
	// pairs, ensure there are no more accounts / slots in the trie.
	if len(keys) == 0 {
		if hasRightElementZk(root, firstKey, true) {
			return false, errors.New("more entries available")
		}
		return false, nil
	}
	var lastKey = keys[len(keys)-1]
	// Special case, there is only one element and two edge keys are same.
	// In this case, we can't construct two edge paths. So handle it here.
	if len(keys) == 1 && bytes.Equal(firstKey, lastKey) {
		if err := verifySingleInput(root, firstKey, keys[0], values[0]); err != nil {
			return false, err
		}
		return hasRightElementZk(root, firstKey, false), nil
	}
	// Ok, in all other cases, we require two edge paths available.
	// First check the validity of edge keys.
	if len(firstKey) != len(lastKey) {
		return false, errors.New("inconsistent edge keys")
	}

	if bytes.Compare(firstKey, lastKey) >= 0 { // left < right
		return false, errors.New("invalid zk path")
	}

	if !(bytes.Compare(firstKey, keys[0]) <= 0 && bytes.Compare(keys[0], lastKey) <= 0 && // startPath <= firstKeyPath <= endPath
		bytes.Compare(firstKey, keys[len(keys)-1]) <= 0 && bytes.Compare(keys[len(keys)-1], lastKey) <= 0) { // startPath <= lastKeyPath <= endPath
		return false, errors.New("invalid range")
	}
	// Remove all internal references. All the removed parts should
	// be re-filled(or re-constructed) by the given leaves range.
	empty := unsetInternalZk(root, firstKey, lastKey)
	// Rebuild the trie with the leaf stream, the shape of trie should be same with the original one.
	var tree *zk.MerkleTree
	if empty {
		tree = zk.NewEmptyMerkleTree()
	} else {
		tree = zk.NewMerkleTree(root)
	}
	if err := verifyRootHash(rootHash, tree, keys, values, configTree); err != nil {
		return false, err
	}
	return hasRightElementZk(tree.RootNode(), lastKey, false), nil
}

func validateKeyValue(keys [][]byte, values [][]byte) error {
	if len(keys) != len(values) {
		return fmt.Errorf("inconsistent proof data, keys: %d, values: %d", len(keys), len(values))
	}
	// Ensure the received batch is monotonic increasing and contains no deletions
	for _, value := range values {
		if len(value) == 0 {
			return errors.New("range contains deletion")
		}
	}
	for i := 0; i < len(keys)-1; i++ {
		if bytes.Compare(keys[i], keys[i+1]) >= 0 {
			return errors.New("range is not monotonically increasing")
		}
	}
	return nil
}

// verifyRootHash updates the tree and verifies that it is equal to rootHash.
func verifyRootHash(rootHash common.Hash, tree *zk.MerkleTree, keys [][]byte, values [][]byte, configTree func(tree *zk.MerkleTree)) error {
	tree.WithMaxLevels(256)
	if configTree != nil {
		configTree(tree)
	}
	for index, key := range keys {
		hash := ZkIteratorKeyToZkHash(common.BytesToHash(key))
		if err := tree.Update(hash[:], values[index]); err != nil {
			return err
		}
	}
	if have, want := tree.Hash(), rootHash[:]; !bytes.Equal(have, want) {
		return fmt.Errorf("invalid proof, want hash %x, got %x", want, have)
	}
	return nil
}

func verifySingleInput(root zk.TreeNode, firstKey []byte, key []byte, value []byte) error {
	if !bytes.Equal(firstKey, key) {
		return errors.New("correct proof but invalid key")
	}
	leaf, ok := zk.NewMerkleTree(root).GetNodeByPath(iteratorKeyToZkTreePath(key)).(*zk.LeafNode)
	if !ok {
		return errors.New("not leaf node")
	}
	_, values, err := zk.MarshalBytes(value)
	if err != nil {
		return err
	}
	if len(leaf.ValuePreimage) == len(values) {
		for i := range leaf.ValuePreimage {
			if !bytes.Equal(leaf.ValuePreimage[i][:], values[i][:]) {
				return errors.New("correct proof but invalid data")
			}
		}
		return nil
	}
	return errors.New("correct proof but invalid data")
}

// resolveRootNode creates a root [zk.TreeNode] with rootHash.
// It then visits the descendants of the root [zk.TreeNode] and converts
// any [zk.HashNode] for which a proof exists to the original [zk.TreeNode].
func resolveRootNode(rootHash common.Hash, proof ethdb.KeyValueReader) (root zk.TreeNode, err error) {
	if root, err = parseTreeNode(zkt.NewHashFromBytes(rootHash[:]), proof); err != nil {
		return nil, err
	} else if root == nil {
		return nil, fmt.Errorf("zk root node (hash %064x) missing", rootHash)
	}
	return root, zk.VisitNode(root, func(node zk.TreeNode, _ zk.TreePath) error {
		if parent, ok := node.(*zk.ParentNode); ok {
			for path, child := range parent.Children() {
				if _, ok := child.(*zk.HashNode); ok {
					child, err := parseTreeNode(child.Hash(), proof)
					if err != nil {
						return err
					}
					if child != nil {
						parent.SetChild(byte(path), child)
					}
				}
			}
		}
		return nil
	})
}

func parseTreeNode(hash *zkt.Hash, proof ethdb.KeyValueReader) (zk.TreeNode, error) {
	buf, _ := proof.Get(hash[:])
	if buf == nil {
		return nil, nil
	}
	node, err := zk.NewTreeNodeFromBlob(buf)
	if err != nil {
		return nil, fmt.Errorf("bad proof zk node %v", err)
	}
	return node, nil
}

// unsetInternalZk is similar to unsetInternal
// All [zk.ParentNode.Children] that have a proof must be decoded to the origin [zk.TreePath], not the [zk.HashNode].
// Converts all nodes between startPath and endPath to [zk.EmptyNode],
// and clears the cached hash in order to recompute the hash (see [zk.ParentNode.SetChild]).
// The [zk.LeafNode] passed to the proof is also subject to the proof, and should be converted to a [zk.EmptyNode].
// A [zk.LeafNode] may be included even if it does not exist between firstKey and lastKey, in which case it should not be converted to an [zk.EmptyNode].
// see unsetLeafIfAfterPath, unsetLeafIfBeforePath
func unsetInternalZk(node zk.TreeNode, firstKey []byte, lastKey []byte) bool {
	startPath, endPath := iteratorKeyToZkTreePath(firstKey), iteratorKeyToZkTreePath(lastKey)
	maxLevel := len(startPath)
	forkLvl := 0
	for lvl := 0; lvl < maxLevel; lvl++ {
		if startPath[lvl] == endPath[lvl] {
			parent, ok := node.(*zk.ParentNode)
			if !ok {
				return lvl == 0
			}
			node = parent.Child(startPath[lvl])
		} else {
			forkLvl = lvl
			break
		}
	}
	if forkNode, ok := node.(*zk.ParentNode); ok {
		unsetLeafIfAfterPath(forkNode, left, startPath)
		unsetLeafIfBeforePath(forkNode, right, endPath)
		startPathNode, endPathNode := forkNode.ChildL(), forkNode.ChildR()
		for lvl := forkLvl + 1; lvl < maxLevel; lvl++ {
			remainNode := false
			if startPathParent, ok := startPathNode.(*zk.ParentNode); ok {
				remainNode = true
				if startPath[lvl] == left {
					startPathParent.SetChild(right, zk.EmptyNodeValue)
				}
				unsetLeafIfAfterPath(startPathParent, startPath[lvl], startPath)
				startPathNode = startPathParent.Child(startPath[lvl])
			}
			if endPathParent, ok := endPathNode.(*zk.ParentNode); ok {
				remainNode = true
				if endPath[lvl] == right {
					endPathParent.SetChild(left, zk.EmptyNodeValue)
				}
				unsetLeafIfBeforePath(endPathParent, endPath[lvl], endPath)
				endPathNode = endPathParent.Child(endPath[lvl])
			}
			if !remainNode {
				break
			}
		}
	}
	return false
}

// hasRightElementZk returns true if there is a [zk.LeafNode] to the right of the given path.
func hasRightElementZk(node zk.TreeNode, key []byte, includeSelf bool) bool {
	path := iteratorKeyToZkTreePath(key)
	for _, p := range path {
		parent, ok := node.(*zk.ParentNode)
		if !ok {
			break
		}
		// If the next path is to the right, we go down without doing anything.
		if p == left {
			switch parent.ChildR().(type) {
			case *zk.ParentNode: // A ParentNode must have at least one LeafNode among its descendants.
				return true
			case *zk.HashNode: // There is a Node that has not been verified in this proof.
				return true
			}
		}
		node = parent.Child(p)
	}
	switch n := node.(type) {
	case *zk.LeafNode:
		if includeSelf {
			return bytes.Compare(path, zk.NewTreePathFromBytes(n.Key)) <= 0 // path <= n.Key
		}
		return bytes.Compare(path, zk.NewTreePathFromBytes(n.Key)) == -1 // path < n.Key
	case *zk.HashNode:
		return true
	}
	return false
}

func iteratorKeyToZkTreePath(key []byte) zk.TreePath {
	return zk.NewTreePathFromHashBig(common.BytesToHash(key))
}

func unsetLeafIfAfterPath(parent *zk.ParentNode, direction byte, path zk.TreePath) {
	if leaf, ok := parent.Child(direction).(*zk.LeafNode); ok {
		if bytes.Compare(path, zk.NewTreePathFromBytes(leaf.Key)) <= 0 {
			parent.SetChild(direction, zk.EmptyNodeValue)
		}
	}
}

func unsetLeafIfBeforePath(parent *zk.ParentNode, direction byte, path zk.TreePath) {
	if leaf, ok := parent.Child(direction).(*zk.LeafNode); ok {
		if bytes.Compare(zk.NewTreePathFromBytes(leaf.Key), path) <= 0 {
			parent.SetChild(direction, zk.EmptyNodeValue)
		}
	}
}
