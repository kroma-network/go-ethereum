package zk

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/kroma-network/zktrie/trie"
	. "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/poseidon"
)

func init() { InitHashScheme(poseidon.HashFixed) }

func newEmptyZkTrie() *trie.ZkTrie {
	// A trie generated from an empty root will not raise an error.
	zktrie, _ := trie.NewZkTrie(Byte32{}, trie.NewZkTrieMemoryDb())
	return zktrie
}

func TestHash(t *testing.T) {
	for leafCount := 0; leafCount <= 200; leafCount++ {
		t.Run(fmt.Sprintf("test leaf count %d", leafCount), func(t *testing.T) { testUpdateAndHash(t, leafCount) })
	}
}

func TestHash1000(t *testing.T) { testUpdateAndHash(t, 1000) }

func testUpdateAndHash(t *testing.T, inputCount int) {
	zktrie, zktree := newEmptyZkTrie(), NewEmptyMerkleTree()
	input := newTestInputFixedCount(inputCount)
	input.applyZkTrie(zktrie).applyZkTrees(zktree)
	if !bytes.Equal(zktrie.Hash(), zktree.Hash()) {
		t.Fatalf("invalid hash. want : %x, but got %x", zktrie.Hash(), zktree.Hash())
	}
}

func TestDelete(t *testing.T) {
	t.Run("leaf count 1", func(t *testing.T) {
		zktree := NewEmptyMerkleTree()
		input := newTestInputFixedCount(1).applyZkTrees(zktree)
		if err := zktree.DeleteByNodeKey(MustNewSecureHash([]byte(input.keys[0]))); err != nil {
			t.Errorf("fail to delete. error : %v", err)
		}
		if !bytes.Equal(zktree.Hash(), common.Hash{}.Bytes()) {
			t.Errorf("root not empty node")
		}
	})
	for leafCount := 10; leafCount <= 100; leafCount++ {
		t.Run(fmt.Sprintf("leaf count %d", leafCount), func(t *testing.T) {
			zktrie, zktree := newEmptyZkTrie(), NewEmptyMerkleTree()
			input := newTestInputFixedCount(leafCount).applyZkTrie(zktrie).applyZkTrees(zktree)
			deleteKeys := input.keys[:leafCount/3]
			for _, key := range deleteKeys {
				zktrie.TryDelete([]byte(key))
				zktree.Delete([]byte(key))
			}
			if !bytes.Equal(zktrie.Hash()[:], zktree.Hash()[:]) {
				t.Errorf("root mismatch!")
			}
		})
	}
}

func TestPushLeaf(t *testing.T) {
	pushLeaf := func(newLeafPath TreePath, oldLeafPath TreePath) (*ParentNode, error) {
		return NewEmptyMerkleTree().WithMaxLevels(4).pushLeaf(
			NewLeafNode(NewHashFromBytes([]byte("new")), 1, []Byte32{*NewByte32FromBytes([]byte{0})}),
			NewLeafNode(NewHashFromBytes([]byte("old")), 1, []Byte32{*NewByte32FromBytes([]byte{1})}),
			newLeafPath,
			oldLeafPath,
			0,
		)
	}
	t.Run("should return ErrReachedMaxLevel", func(t *testing.T) {
		_, err := pushLeaf([]byte{0, 1, 0, 1}, []byte{0, 1, 0, 1})
		if !errors.Is(err, trie.ErrReachedMaxLevel) {
			t.Errorf("pushLeaf test failed.")
		}
	})
	t.Run("insert max level", func(t *testing.T) {
		parent, err := pushLeaf([]byte{0, 1, 0, 0}, []byte{0, 1, 0, 1})
		if err != nil {
			t.Errorf("pushLeaf test failed.")
		}
		for _, p := range []byte{0, 1, 0} {
			parent = parent.Child(p).(*ParentNode)
		}
		for _, node := range parent.Children() {
			if _, ok := node.(*LeafNode); !ok {
				t.Errorf("")
			}
		}
	})
}

func TestProof(t *testing.T) {
	for leafCount := 1; leafCount <= 200; leafCount++ {
		t.Run(fmt.Sprintf("test leaf count %d", leafCount), func(t *testing.T) {
			input := newTestInputFixedCount(leafCount)
			zktrie, zktree := newEmptyZkTrie(), NewEmptyMerkleTree()
			input.applyZkTrie(zktrie).applyZkTrees(zktree)
			for tc := 0; tc < int(math.Min(float64(leafCount), float64(10))); tc++ {
				testKey := []byte(input.keys[rand.Intn(leafCount)])
				trieHashs, treeHashs := *new([]*Hash), *new([]*Hash)
				zktrie.Prove(testKey, 0, func(node *trie.Node) error {
					hash, err := node.NodeHash()
					trieHashs = append(trieHashs, hash)
					return err
				})
				zktree.Prove(testKey, func(node TreeNode) error {
					treeHashs = append(treeHashs, node.Hash())
					return nil
				})
				if len(trieHashs) != len(treeHashs) {
					t.Errorf("proof count mismatch. trie %d, tree %d", len(trieHashs), len(treeHashs))
				}
				for i := 0; i < len(trieHashs); i++ {
					if !bytes.Equal(trieHashs[i].Bytes(), treeHashs[i].Bytes()) {
						t.Errorf("index %d proof mismatch", i)
					}
				}
			}
		})
	}
}

func BenchmarkUpdateAndHash(b *testing.B) {
	type testTree struct {
		Update func(key []byte, vFlag uint32, vPreimage []Byte32) error
		Hash   func() []byte
	}
	makeTest := func(input *testInput, initTree func() *testTree) func(b *testing.B) {
		return func(sub *testing.B) {
			for i := 0; i < sub.N; i++ {
				tree := initTree()
				sub.ReportAllocs()
				sub.StartTimer()
				input.forEach(func(k, v []byte) {
					tree.Update(k, 1, []Byte32{*NewByte32FromBytes(v)})
				})
				tree.Hash()
				sub.StopTimer()
			}
		}
	}
	for i := 0; i < 10; i++ {
		input := newTestInputFixedCount(200)
		b.Run("trie", makeTest(input, func() *testTree {
			zkTrie := newEmptyZkTrie()
			return &testTree{Update: zkTrie.TryUpdate, Hash: zkTrie.Hash}
		}))

		b.Run("tree", makeTest(input, func() *testTree {
			tree := NewEmptyMerkleTree()
			return &testTree{Update: tree.UpdateUnsafe, Hash: tree.Hash}
		}))
	}
}
