package zk

import (
	"math/rand"

	zktrie "github.com/kroma-network/zktrie/trie"
	. "github.com/kroma-network/zktrie/types"
)

type testInput struct {
	keys   []string
	values []string
}

func newTestInputFixedCount(count int) *testInput {
	return (&testInput{}).generate(count, rand.New(rand.NewSource(1)))
}

func (t *testInput) generate(count int, rnd *rand.Rand) *testInput {
	for i := 0; i < count; i++ {
		t.keys = append(t.keys, randomString(rnd, 32))
		t.values = append(t.values, randomString(rnd, 32))
	}
	return t
}

func (t *testInput) applyZkTrie(trie *zktrie.ZkTrie) *testInput {
	t.forEach(func(k, v []byte) { trie.TryUpdate(k, 1, []Byte32{*NewByte32FromBytes(v)}) })
	return t
}
func (t *testInput) applyZkTrees(trees ...*MerkleTree) *testInput {
	t.forEach(func(k, v []byte) {
		for _, tree := range trees {
			tree.Update(MustNewSecureHash(k)[:], v)
		}
	})
	return t
}

func (t *testInput) forEach(callback func(k, v []byte)) {
	for i := 0; i < t.len(); i++ {
		callback([]byte(t.keys[i]), []byte(t.values[i]))
	}
}

func (t *testInput) len() int { return len(t.keys) }

func randomString(rnd *rand.Rand, strlen int) string {
	b := make([]byte, strlen)
	rnd.Read(b)
	return string(b)
}
