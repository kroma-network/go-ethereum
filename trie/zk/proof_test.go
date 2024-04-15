package zk

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/kroma-network/zktrie/trie"
	zkt "github.com/kroma-network/zktrie/types"
)

func TestVerifyProof(t *testing.T) {
	for leafCount := 2; leafCount <= 100; leafCount++ {
		t.Run(fmt.Sprintf("leaf count %v", leafCount), func(t *testing.T) {
			testVerifyProof(t, newTestInputFixedCount(leafCount))
		})
	}
}

func testVerifyProof(t *testing.T, input *testInput) {
	zktrie, zktree := newEmptyZkTrie(), NewEmptyMerkleTree()
	input.applyZkTrie(zktrie).applyZkTrees(zktree)
	for tc := 0; tc < int(math.Min(float64(input.len()), float64(10))); tc++ {
		testKey := []byte(input.keys[rand.Intn(input.len())])
		testHashKey := MustNewSecureHash(testKey)
		trieProof, trieLeaf := createTrieProof(testHashKey, zktrie)
		treeProof := createTreeProof(testHashKey, zktree)
		if trieProof.Existence != treeProof.Existence() {
			t.Errorf("Existence mismatch %v != %v", trieProof.Existence, treeProof.Existence())
		}
		if len(trieProof.Siblings) != len(treeProof.Siblings) {
			t.Errorf("Siblings length mismatch %v != %v", len(trieProof.Siblings), len(treeProof.Siblings))
		}
		for i, sibling := range trieProof.Siblings {
			if !bytes.Equal(sibling[:], treeProof.Siblings[i][:]) {
				t.Errorf("sibling mismatch %v != %v", sibling, treeProof.Siblings[i])
			}
		}
		if !bytes.Equal(must(trieLeaf.NodeHash())[:], treeProof.Leaf.Hash()[:]) {
			t.Errorf("leaf node hash mismatch")
		}
		if err := verifyProof(zkt.NewHashFromBytes(zktree.Hash()), treeProof, NewPoseidonHasher()); err == nil {
			if !trie.VerifyProofZkTrie(zkt.NewHashFromBytes(zktrie.Hash()), trieProof, trieLeaf) {
				t.Errorf("")
			}
		} else {
			if trie.VerifyProofZkTrie(zkt.NewHashFromBytes(zktrie.Hash()), trieProof, trieLeaf) {
				t.Errorf("")
			}
		}
	}
}

func createTrieProof(key *zkt.Hash, zktrie *trie.ZkTrie) (*trie.Proof, *trie.Node) {
	proofDb := make(map[string][]byte)
	zktrie.Prove(key.Bytes(), 0, func(node *trie.Node) error {
		proofDb[string(must(node.NodeHash())[:])] = node.CanonicalValue()
		return nil
	})
	proof, node, _ := trie.BuildZkTrieProof(
		zkt.NewHashFromBytes(zktrie.Hash()),
		key,
		trie.GetPath(248, key[:]),
		func(key *zkt.Hash) (*trie.Node, error) { return trie.NewNodeFromBytes(proofDb[string(key[:])]) },
	)
	return proof, node
}

func createTreeProof(key *zkt.Hash, tree *MerkleTree) *Proof {
	proofDb := make(map[string][]byte)
	tree.Prove(key[:], func(node TreeNode) error {
		proofDb[string(node.Hash()[:])] = node.CanonicalValue()
		return nil
	})
	return must(NewProof(zkt.NewHashFromBytes(tree.Hash()), key[:], func(key []byte) ([]byte, error) { return proofDb[string(key)], nil }, nil))
}
