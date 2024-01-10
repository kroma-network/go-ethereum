package zk

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/kroma-network/zktrie/trie"
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/log"
)

// MerkleTree wraps a tree with key hashing.
// All access operations hash the key using poseidon computeNodeHash
//
// MerkleTree is not safe for concurrent use.
type MerkleTree struct {
	rootNode TreeNode
	// Sets the maximum depth of the tree. Exceeding it will result in a [trie.ErrReachedMaxLevel] error.
	maxLevels int
	// findBlobByHash is a function that reads tree blob data by [TreeNode.Hash].
	// It exists to decode the HashNode into the original TreeNode, so it is not needed if the HashNode cannot be created (e.g, rootNode is an EmptyNode).
	findBlobByHash func(hash []byte) ([]byte, error)
}

func NewMerkleTreeFromHash(rootHash *zkt.Hash, findBlobByHash func(hash []byte) ([]byte, error)) (*MerkleTree, error) {
	if bytes.Equal(rootHash.Bytes(), zkt.HashZero.Bytes()) {
		return NewEmptyMerkleTree(), nil
	}
	blob, err := findBlobByHash(rootHash[:])
	if err != nil {
		return nil, err
	}
	rootNode, err := NewTreeNodeFromBlob(blob)
	if err != nil {
		return nil, err
	}
	setNodeHash(rootNode, rootHash)
	return NewMerkleTree(rootNode).WithNodeBlobFinder(findBlobByHash), nil
}

func NewEmptyMerkleTree() *MerkleTree { return NewMerkleTree(EmptyNodeValue) }
func NewMerkleTree(rootNode TreeNode) *MerkleTree {
	return &MerkleTree{rootNode: rootNode, maxLevels: trie.NodeKeyValidBytes * 8}
}

func (t *MerkleTree) RootNode() TreeNode { return t.rootNode }
func (t *MerkleTree) MaxLevels() int     { return t.maxLevels }

func (t *MerkleTree) WithMaxLevels(maxLevels int) *MerkleTree {
	t.maxLevels = maxLevels
	return t
}

func (t *MerkleTree) WithNodeBlobFinder(findBlobByHash func(hash []byte) ([]byte, error)) *MerkleTree {
	t.findBlobByHash = findBlobByHash
	return t
}

// Hash returns the root hash of the MerkleTree.
// It does not write to the database and can be used even if the MerkleTree doesn't have one.
func (t *MerkleTree) Hash() []byte {
	if err := t.ComputeAllNodeHash(nil); err != nil {
		log.Error("Failed to compute hash", "err", err)
		return []byte{}
	}
	return t.rootNode.Hash().Bytes()
}

// ComputeAllNodeHash Compute the node hash, and call the handleDirtyNode function.
// handleDirtyNode is only called if the node hash is newly computed.
func (t *MerkleTree) ComputeAllNodeHash(handleDirtyNode func(dirtyNode TreeNode) error) error {
	return computeNodeHash(t.rootNode, handleDirtyNode)
}

func (t *MerkleTree) MustGet(key []byte) []byte {
	if blob, err := t.Get(key); err != nil {
		log.Error("MerkleTree.MustGet", "error", err)
		return nil
	} else {
		return blob
	}
}

// Get returns the value for key stored in the tree.
// The value bytes must not be modified by the caller.
func (t *MerkleTree) Get(key []byte) ([]byte, error) {
	node, err := t.GetLeafNode(key)
	if err != nil {
		if errors.Is(err, trie.ErrKeyNotFound) {
			return nil, nil // according to https://github.com/ethereum/go-ethereum/blob/37f9d25ba027356457953eab5f181c98b46e9988/trie/trie.go#L135
		}
		return nil, err
	}
	return node.Data(), nil
}

// GetLeafNode is more underlying method than Get, which obtain a leaf node or nil if not exist.
func (t *MerkleTree) GetLeafNode(key []byte) (*LeafNode, error) {
	node, path := t.rootNode, t.newTreePath(key)
	for lvl, p := range path {
		switch n := node.(type) {
		case *ParentNode:
			node = t.getChild(n, p)
		case *LeafNode:
			if !bytes.Equal(key[:], n.Key[:]) {
				return nil, trie.ErrKeyNotFound
			}
			return n, nil
		case *EmptyNode:
			return nil, trie.ErrKeyNotFound
		case *HashNode:
			return nil, fmt.Errorf("GetLeafNode: encounter hash node. level %d, path %v", lvl, path[:lvl])
		default:
			return nil, trie.ErrInvalidNodeFound
		}
	}
	return nil, trie.ErrKeyNotFound
}

// GetNodeByPath returns the tree node at the end of the path
func (t *MerkleTree) GetNodeByPath(path TreePath) TreeNode {
	node := t.rootNode
	for _, p := range path {
		if parent, ok := node.(*ParentNode); ok {
			node = t.getChild(parent, p)
		} else {
			break
		}
	}
	return node
}

func (t *MerkleTree) MustUpdate(key, value []byte) {
	if err := t.Update(key, value); err != nil {
		log.Error("MerkleTree.MustUpdate", "error", err)
	}
}

// Update associates key with value in the tree. Subsequent calls to Get will return value.
//
// The value bytes must not be modified by the caller while they are stored in the tree.
func (t *MerkleTree) Update(key []byte, value []byte) error {
	if leaf, err := NewLeafNode(key, value); err != nil {
		return err
	} else {
		return t.AddLeaf(leaf)
	}
}

// AddLeaf updates a nodeKey & value into the MerkleTree.
// Here the `nodeKey` determines the path from the Root to the Leaf.
func (t *MerkleTree) AddLeaf(leaf *LeafNode) error {
	newRoot, err := t.addLeaf(leaf, t.rootNode, 0, t.newTreePath(leaf.Key), true)
	// sanity check
	if err != nil {
		if errors.Is(err, trie.ErrEntryIndexAlreadyExists) {
			panic("Encounter unexpected errortype: ErrEntryIndexAlreadyExists")
		}
		return err
	}
	t.rootNode = newRoot
	return nil
}

// addLeaf recursively adds a newLeaf in the MT while updating the path, and returns the node with added leaf.
func (t *MerkleTree) addLeaf(
	newLeaf *LeafNode,
	currNode TreeNode,
	lvl int,
	path TreePath,
	forceUpdate bool,
) (TreeNode, error) {
	if lvl > t.maxLevels-1 {
		return nil, trie.ErrReachedMaxLevel
	}
	switch n := currNode.(type) {
	case *ParentNode:
		newNode, err := t.addLeaf(newLeaf, t.getChild(n, path.Get(lvl)), lvl+1, path, forceUpdate)
		if err != nil {
			log.Error("fail to addLeaf", "err", err, "level", lvl)
			return nil, err
		}
		n.SetChild(path.Get(lvl), newNode) // Update the node to reflect the modified child
		return n, nil
	case *LeafNode:
		if !bytes.Equal(newLeaf.Key, n.Key) {
			pathOldLeaf := t.newTreePath(n.Key)
			// We need to push newLeaf down until its path diverges from n's path
			return t.pushLeaf(newLeaf, n, path, pathOldLeaf, lvl)
		}
		if bytes.Equal(newLeaf.CanonicalValue(), n.CanonicalValue()) {
			return currNode, nil // do nothing, duplicate entry
		}
		if forceUpdate {
			return newLeaf, nil
		}
		return nil, trie.ErrEntryIndexAlreadyExists
	case *EmptyNode:
		return newLeaf, nil
	case *HashNode:
		return nil, fmt.Errorf("addLeaf: encounter hash node. level %d, path %d", lvl, path[:lvl])
	default:
		return nil, trie.ErrInvalidNodeFound
	}
}

// pushLeaf pushes an existing oldLeaf down until its path diverges from newLeaf,
// at which point both leafs are stored, all while updating the path.
// pushLeaf returns the parent node of oldLeaf and newLeaf
func (t *MerkleTree) pushLeaf(
	newLeaf *LeafNode,
	oldLeaf *LeafNode,
	newLeafPath TreePath,
	oldLeafPath TreePath,
	oldLeafLvl int,
) (*ParentNode, error) {
	maxLevel := min(len(newLeafPath), len(oldLeafPath), t.maxLevels)
	forkLvl := oldLeafLvl
	for ; newLeafPath[forkLvl] == oldLeafPath[forkLvl] && forkLvl < maxLevel-1; forkLvl++ {
	}
	if forkLvl == maxLevel-1 && newLeafPath[forkLvl] == oldLeafPath[forkLvl] {
		return nil, trie.ErrReachedMaxLevel
	}
	parentNode := newParentNode(newLeafPath.Get(forkLvl), newLeaf, oldLeafPath.Get(forkLvl), oldLeaf)
	for lvl := forkLvl - 1; lvl >= oldLeafLvl; lvl-- {
		parentNode = newParentNode(newLeafPath.Get(lvl), parentNode, newLeafPath.GetOther(lvl), EmptyNodeValue)
	}
	return parentNode, nil
}

func (t *MerkleTree) MustDelete(key []byte) {
	if err := t.Delete(key); err != nil {
		log.Error("MerkleTree.MustDelete", "error", err)
	}
}

// Delete removes the specified Key from the MerkleTree and updates the path
// from the deleted key to the Root with the new values. This method removes
// the key from the MerkleTree, but does not remove the old nodes from the
// key-value database; this means that if the tree is accessed by an old Root
// where the key was not deleted yet, the key will still exist. If is desired
// to remove the key-values from the database that are not under the current
// Root, an option could be to dump all the leafs (using mt.DumpLeafs) and
// import them in a new MerkleTree in a new database (using
// mt.ImportDumpedLeafs), but this will lose all the Root history of the MerkleTree
func (t *MerkleTree) Delete(key []byte) error {
	node, path, pathNodes := t.rootNode, t.newTreePath(key), *new([]*ParentNode)
	for _, p := range path {
		switch n := node.(type) {
		case *ParentNode:
			pathNodes = append(pathNodes, n)
			node = t.getChild(n, p)
		case *LeafNode:
			if bytes.Equal(key, n.Key) {
				t.rmAndUpload(path, pathNodes)
				return nil
			}
			return trie.ErrKeyNotFound
		case *EmptyNode:
			return trie.ErrKeyNotFound
		case *HashNode:
			return trie.ErrKeyNotFound
		default:
			return trie.ErrInvalidNodeFound
		}
	}
	return trie.ErrKeyNotFound
}

// rmAndUpload removes the key, and goes up until the root updating all the nodes with the new values.
// Following the traditional zktrie implementation, clearing a tree node does not clear the storage area.
func (t *MerkleTree) rmAndUpload(path TreePath, pathNodes []*ParentNode) {
	switch len(pathNodes) {
	case 0: // The leaf node you want to remove is root node.
		t.rootNode = EmptyNodeValue
	case 1:
		// root (ParentNode) --- LeafNode or ParentNode (promoted to root node)
		//                    |- LeafNode (deleted)
		t.rootNode = t.getChild(pathNodes[0], path.GetOther(0))
	default:
		lastSibling := t.getChild(pathNodes[len(pathNodes)-1], path.GetOther(len(pathNodes)-1))
		defer func() {
			if t.rootNode != lastSibling {
				for _, node := range pathNodes {
					clearNodeHash(node)
				}
			}
		}()
		if _, ok := lastSibling.(*ParentNode); ok {
			// The sibling of the node to be deleted is the parent node. Add an EmptyNode to the location to be deleted.
			// last ParentNode --- LeafNode (deleted) <- set empty node
			//                  |- ParentNode (sibling)
			pathNodes[len(pathNodes)-1].SetChild(path.Get(len(pathNodes)-1), EmptyNodeValue)
			return
		}
		// The sibling of the node to be deleted is a LeafNode.
		// Find a parent node whose sibling is not an empty node.
		// last ParentNode --- LeafNode (deleted)
		//                  |- LeafNode (sibling)
		for i := len(pathNodes) - 2; i >= 0; i-- { // start parent of last ParentNode
			clearNodeHash(pathNodes[i])
			sibling := t.getChild(pathNodes[i], path.GetOther(i))
			if _, ok := sibling.(*EmptyNode); ok {
				// To shorten the path as much as possible, if the sibling node is an empty node, it will be moved up.
				// (with only one LeafNode among Parent's children).
				continue
			}
			// A node that can branch to a state with two or more LeafNodes.
			// ... - ParentNode
			//           |- last ParentNode (replaced to sibling)
			//           |           |- LeafNode (deleted)
			//           |           |- LeafNode (sibling)
			//           |- not EmptyNode
			pathNodes[i].SetChild(path.Get(i), lastSibling)
			pathNodes = pathNodes[:i]
			return
		}
		// if all sibling is zero, stop and store the sibling of the deleted leaf as root
		t.rootNode = lastSibling
	}
}

// Prove constructs a merkle proof for key. The result contains all encoded nodes
// on the path to the value at key. The value itself is also included in the last
// node and can be retrieved by verifying the proof.
//
// If the tree does not contain a value for key, the returned proof contains all
// nodes of the longest existing prefix of the key (at least the root node), ending
// with the node that proves the absence of the key.
func (t *MerkleTree) Prove(key []byte, writeNode func(TreeNode) error) error {
	// notice Prove in secure tree "pass through" the key instead of secure it
	// this keep consistent behavior with geth's secure trie
	if err := t.ComputeAllNodeHash(nil); err != nil {
		return err
	}
	node := t.rootNode
	for _, p := range t.newTreePath(key) {
		// TODO: notice here we may have broken some implicit on the proofDb:
		// the key is not keccak(value) and it even can not be derived from the value by any means without an actual decoding
		if err := writeNode(node); err != nil {
			return err
		}
		switch n := node.(type) {
		case *ParentNode:
			node = t.getChild(n, p)
		case *LeafNode:
			return nil
		case *EmptyNode:
			return nil
		case *HashNode:
			return nil
		default:
			return trie.ErrInvalidNodeFound
		}
	}
	return nil
}

func (t *MerkleTree) Copy() *MerkleTree {
	return &MerkleTree{
		rootNode:       t.rootNode,
		maxLevels:      t.maxLevels,
		findBlobByHash: t.findBlobByHash,
	}
}

// getChild If the child node is a hash node, decode it and update the parent node.
func (t *MerkleTree) getChild(node *ParentNode, path byte) TreeNode {
	if hashNode, ok := node.Child(path).(*HashNode); ok {
		if child := t.findNodeByHash(hashNode.Hash()); child != nil {
			node.SetChild(path, child)
		}
	}
	return node.Child(path)
}

// findNodeByHash finds a treeNode by hash. The TreeNode found is not always the most recent state node.
// If compatibility mode is enabled, the intermediate state of the tree can be read from the dirtyNode.
// The [ParentNode.Children] present in the dirtyNode must be HashNode (see copyNode).
func (t *MerkleTree) findNodeByHash(hash *zkt.Hash) TreeNode {
	if hash == nil || t.findBlobByHash == nil {
		return nil
	}
	blob, err := t.findBlobByHash(hash[:])
	if err != nil {
		log.Error("fail to read blob by hash", "hash", hash, "err", err)
		return nil
	}
	node, err := NewTreeNodeFromBlob(blob)
	if err != nil {
		log.Error("fail to decode node from blob", "hash", hash, "blob", blob, "err", err)
		return nil
	}
	setNodeHash(node, hash)
	return node
}

func (t *MerkleTree) newTreePath(key []byte) TreePath {
	return NewTreePathFromBytesAndMaxLevel(key, t.maxLevels)
}
