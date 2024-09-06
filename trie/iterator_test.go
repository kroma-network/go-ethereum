// Copyright 2014 The go-ethereum Authors
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
	crand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	zkt "github.com/kroma-network/zktrie/types"

	"golang.org/x/exp/slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

func TestEmptyIterator(t *testing.T) {
	trie := NewEmpty(NewDatabase(rawdb.NewMemoryDatabase(), nil))
	iter := trie.MustNodeIterator(nil)

	seen := make(map[string]struct{})
	for iter.Next(true) {
		seen[string(iter.Path())] = struct{}{}
	}
	if len(seen) != 0 {
		t.Fatal("Unexpected trie node iterated")
	}
}

func TestIterator(t *testing.T) {
	db := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	trie := NewEmpty(db)
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
	}
	all := make(map[string]string)
	for _, val := range vals {
		all[val.k] = val.v
		trie.MustUpdate([]byte(val.k), []byte(val.v))
	}
	root, nodes, _ := trie.Commit(false)
	db.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil)

	trie, _ = New(TrieID(root), db)
	found := make(map[string]string)
	it := NewIterator(trie.MustNodeIterator(nil))
	for it.Next() {
		found[string(it.Key)] = string(it.Value)
	}

	for k, v := range all {
		if found[k] != v {
			t.Errorf("iterator value mismatch for %s: got %q want %q", k, found[k], v)
		}
	}
}

type kv struct {
	k, v []byte
	t    bool
}

func (k *kv) cmp(other *kv) int {
	return bytes.Compare(k.k, other.k)
}

func TestIteratorLargeData(t *testing.T) {
	trie := NewEmpty(NewDatabase(rawdb.NewMemoryDatabase(), nil))
	vals := make(map[string]*kv)

	for i := byte(0); i < 255; i++ {
		value := &kv{common.LeftPadBytes([]byte{i}, 32), []byte{i}, false}
		value2 := &kv{common.LeftPadBytes([]byte{10, i}, 32), []byte{i}, false}
		trie.MustUpdate(value.k, value.v)
		trie.MustUpdate(value2.k, value2.v)
		vals[string(value.k)] = value
		vals[string(value2.k)] = value2
	}

	it := NewIterator(trie.MustNodeIterator(nil))
	for it.Next() {
		vals[string(it.Key)].t = true
	}

	var untouched []*kv
	for _, value := range vals {
		if !value.t {
			untouched = append(untouched, value)
		}
	}

	if len(untouched) > 0 {
		t.Errorf("Missed %d nodes", len(untouched))
		for _, value := range untouched {
			t.Error(value)
		}
	}
}

type iterationElement struct {
	hash common.Hash
	path []byte
	blob []byte
}

// Tests that the node iterator indeed walks over the entire database contents.
func TestNodeIteratorCoverage(t *testing.T) {
	testNodeIteratorCoverage(t, rawdb.HashScheme)
	testNodeIteratorCoverage(t, rawdb.PathScheme)
}

func testNodeIteratorCoverage(t *testing.T, scheme string) {
	// Create some arbitrary test trie to iterate
	db, nodeDb, trie, _ := makeTestTrie(scheme)

	// Gather all the node hashes found by the iterator
	var elements = make(map[common.Hash]iterationElement)
	for it := trie.MustNodeIterator(nil); it.Next(true); {
		if it.Hash() != (common.Hash{}) {
			elements[it.Hash()] = iterationElement{
				hash: it.Hash(),
				path: common.CopyBytes(it.Path()),
				blob: common.CopyBytes(it.NodeBlob()),
			}
		}
	}
	// Cross check the hashes and the database itself
	reader, err := nodeDb.Reader(trie.Hash())
	if err != nil {
		t.Fatalf("state is not available %x", trie.Hash())
	}
	for _, element := range elements {
		if blob, err := reader.Node(common.Hash{}, element.path, element.hash); err != nil {
			t.Errorf("failed to retrieve reported node %x: %v", element.hash, err)
		} else if !bytes.Equal(blob, element.blob) {
			t.Errorf("node blob is different, want %v got %v", element.blob, blob)
		}
	}
	var (
		count int
		it    = db.NewIterator(nil, nil)
	)
	for it.Next() {
		res, _, _ := isTrieNode(nodeDb.Scheme(), it.Key(), it.Value())
		if !res {
			continue
		}
		count += 1
		if elem, ok := elements[crypto.Keccak256Hash(it.Value())]; !ok {
			t.Error("state entry not reported")
		} else if !bytes.Equal(it.Value(), elem.blob) {
			t.Errorf("node blob is different, want %v got %v", elem.blob, it.Value())
		}
	}
	it.Release()
	if count != len(elements) {
		t.Errorf("state entry is mismatched %d %d", count, len(elements))
	}
}

type kvs struct{ k, v string }

var testdata1 = []kvs{
	{"barb", "ba"},
	{"bard", "bc"},
	{"bars", "bb"},
	{"bar", "b"},
	{"fab", "z"},
	{"food", "ab"},
	{"foos", "aa"},
	{"foo", "a"},
}

var testdata2 = []kvs{
	{"aardvark", "c"},
	{"bar", "b"},
	{"barb", "bd"},
	{"bars", "be"},
	{"fab", "z"},
	{"foo", "a"},
	{"foos", "aa"},
	{"food", "ab"},
	{"jars", "d"},
}

func TestIteratorSeek(t *testing.T) {
	trie := NewEmpty(NewDatabase(rawdb.NewMemoryDatabase(), nil))
	for _, val := range testdata1 {
		trie.MustUpdate([]byte(val.k), []byte(val.v))
	}

	// Seek to the middle.
	it := NewIterator(trie.MustNodeIterator([]byte("fab")))
	if err := checkIteratorOrder(testdata1[4:], it); err != nil {
		t.Fatal(err)
	}

	// Seek to a non-existent key.
	it = NewIterator(trie.MustNodeIterator([]byte("barc")))
	if err := checkIteratorOrder(testdata1[1:], it); err != nil {
		t.Fatal(err)
	}

	// Seek beyond the end.
	it = NewIterator(trie.MustNodeIterator([]byte("z")))
	if err := checkIteratorOrder(nil, it); err != nil {
		t.Fatal(err)
	}
}

func checkIteratorOrder(want []kvs, it *Iterator) error {
	for it.Next() {
		if len(want) == 0 {
			return fmt.Errorf("didn't expect any more values, got key %q", it.Key)
		}
		if !bytes.Equal(it.Key, []byte(want[0].k)) {
			return fmt.Errorf("wrong key: got %q, want %q", it.Key, want[0].k)
		}
		want = want[1:]
	}
	if len(want) > 0 {
		return fmt.Errorf("iterator ended early, want key %q", want[0])
	}
	return nil
}

func TestDifferenceIterator(t *testing.T) {
	dba := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	triea := NewEmpty(dba)
	for _, val := range testdata1 {
		triea.MustUpdate([]byte(val.k), []byte(val.v))
	}
	rootA, nodesA, _ := triea.Commit(false)
	dba.Update(rootA, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodesA), nil)
	triea, _ = New(TrieID(rootA), dba)

	dbb := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	trieb := NewEmpty(dbb)
	for _, val := range testdata2 {
		trieb.MustUpdate([]byte(val.k), []byte(val.v))
	}
	rootB, nodesB, _ := trieb.Commit(false)
	dbb.Update(rootB, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodesB), nil)
	trieb, _ = New(TrieID(rootB), dbb)

	found := make(map[string]string)
	di, _ := NewDifferenceIterator(triea.MustNodeIterator(nil), trieb.MustNodeIterator(nil))
	it := NewIterator(di)
	for it.Next() {
		found[string(it.Key)] = string(it.Value)
	}

	all := []struct{ k, v string }{
		{"aardvark", "c"},
		{"barb", "bd"},
		{"bars", "be"},
		{"jars", "d"},
	}
	for _, item := range all {
		if found[item.k] != item.v {
			t.Errorf("iterator value mismatch for %s: got %v want %v", item.k, found[item.k], item.v)
		}
	}
	if len(found) != len(all) {
		t.Errorf("iterator count mismatch: got %d values, want %d", len(found), len(all))
	}
}

func TestUnionIterator(t *testing.T) {
	dba := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	triea := NewEmpty(dba)
	for _, val := range testdata1 {
		triea.MustUpdate([]byte(val.k), []byte(val.v))
	}
	rootA, nodesA, _ := triea.Commit(false)
	dba.Update(rootA, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodesA), nil)
	triea, _ = New(TrieID(rootA), dba)

	dbb := NewDatabase(rawdb.NewMemoryDatabase(), nil)
	trieb := NewEmpty(dbb)
	for _, val := range testdata2 {
		trieb.MustUpdate([]byte(val.k), []byte(val.v))
	}
	rootB, nodesB, _ := trieb.Commit(false)
	dbb.Update(rootB, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodesB), nil)
	trieb, _ = New(TrieID(rootB), dbb)

	di, _ := NewUnionIterator([]NodeIterator{triea.MustNodeIterator(nil), trieb.MustNodeIterator(nil)})
	it := NewIterator(di)

	all := []struct{ k, v string }{
		{"aardvark", "c"},
		{"barb", "ba"},
		{"barb", "bd"},
		{"bard", "bc"},
		{"bars", "bb"},
		{"bars", "be"},
		{"bar", "b"},
		{"fab", "z"},
		{"food", "ab"},
		{"foos", "aa"},
		{"foo", "a"},
		{"jars", "d"},
	}

	for i, kv := range all {
		if !it.Next() {
			t.Errorf("Iterator ends prematurely at element %d", i)
		}
		if kv.k != string(it.Key) {
			t.Errorf("iterator value mismatch for element %d: got key %s want %s", i, it.Key, kv.k)
		}
		if kv.v != string(it.Value) {
			t.Errorf("iterator value mismatch for element %d: got value %s want %s", i, it.Value, kv.v)
		}
	}
	if it.Next() {
		t.Errorf("Iterator returned extra values.")
	}
}

func TestIteratorNoDups(t *testing.T) {
	tr := NewEmpty(NewDatabase(rawdb.NewMemoryDatabase(), nil))
	for _, val := range testdata1 {
		tr.MustUpdate([]byte(val.k), []byte(val.v))
	}
	checkIteratorNoDups(t, tr.MustNodeIterator(nil), nil)
}

// This test checks that nodeIterator.Next can be retried after inserting missing trie nodes.
func TestIteratorContinueAfterError(t *testing.T) {
	testIteratorContinueAfterError(t, false, rawdb.HashScheme)
	testIteratorContinueAfterError(t, true, rawdb.HashScheme)
	testIteratorContinueAfterError(t, false, rawdb.PathScheme)
	testIteratorContinueAfterError(t, true, rawdb.PathScheme)
}

func testIteratorContinueAfterError(t *testing.T, memonly bool, scheme string) {
	diskdb := rawdb.NewMemoryDatabase()
	tdb := newTestDatabase(diskdb, scheme)

	tr := NewEmpty(tdb)
	for _, val := range testdata1 {
		tr.MustUpdate([]byte(val.k), []byte(val.v))
	}
	root, nodes, _ := tr.Commit(false)
	tdb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil)
	if !memonly {
		tdb.Commit(root, false)
	}
	tr, _ = New(TrieID(root), tdb)
	wantNodeCount := checkIteratorNoDups(t, tr.MustNodeIterator(nil), nil)

	var (
		paths  [][]byte
		hashes []common.Hash
	)
	if memonly {
		for path, n := range nodes.Nodes {
			paths = append(paths, []byte(path))
			hashes = append(hashes, n.Hash)
		}
	} else {
		it := diskdb.NewIterator(nil, nil)
		for it.Next() {
			ok, path, hash := isTrieNode(tdb.Scheme(), it.Key(), it.Value())
			if !ok {
				continue
			}
			paths = append(paths, path)
			hashes = append(hashes, hash)
		}
		it.Release()
	}
	for i := 0; i < 20; i++ {
		// Create trie that will load all nodes from DB.
		tr, _ := New(TrieID(tr.Hash()), tdb)

		// Remove a random node from the database. It can't be the root node
		// because that one is already loaded.
		var (
			rval  []byte
			rpath []byte
			rhash common.Hash
		)
		for {
			if memonly {
				rpath = paths[rand.Intn(len(paths))]
				n := nodes.Nodes[string(rpath)]
				if n == nil {
					continue
				}
				rhash = n.Hash
			} else {
				index := rand.Intn(len(paths))
				rpath = paths[index]
				rhash = hashes[index]
			}
			if rhash != tr.Hash() {
				break
			}
		}
		if memonly {
			tr.reader.banned = map[string]struct{}{string(rpath): {}}
		} else {
			rval = rawdb.ReadTrieNode(diskdb, common.Hash{}, rpath, rhash, tdb.Scheme())
			rawdb.DeleteTrieNode(diskdb, common.Hash{}, rpath, rhash, tdb.Scheme())
		}
		// Iterate until the error is hit.
		seen := make(map[string]bool)
		it := tr.MustNodeIterator(nil)
		checkIteratorNoDups(t, it, seen)
		missing, ok := it.Error().(*MissingNodeError)
		if !ok || missing.NodeHash != rhash {
			t.Fatal("didn't hit missing node, got", it.Error())
		}

		// Add the node back and continue iteration.
		if memonly {
			delete(tr.reader.banned, string(rpath))
		} else {
			rawdb.WriteTrieNode(diskdb, common.Hash{}, rpath, rhash, rval, tdb.Scheme())
		}
		checkIteratorNoDups(t, it, seen)
		if it.Error() != nil {
			t.Fatal("unexpected error", it.Error())
		}
		if len(seen) != wantNodeCount {
			t.Fatal("wrong node iteration count, got", len(seen), "want", wantNodeCount)
		}
	}
}

// Similar to the test above, this one checks that failure to create nodeIterator at a
// certain key prefix behaves correctly when Next is called. The expectation is that Next
// should retry seeking before returning true for the first time.
func TestIteratorContinueAfterSeekError(t *testing.T) {
	testIteratorContinueAfterSeekError(t, false, rawdb.HashScheme)
	testIteratorContinueAfterSeekError(t, true, rawdb.HashScheme)
	testIteratorContinueAfterSeekError(t, false, rawdb.PathScheme)
	testIteratorContinueAfterSeekError(t, true, rawdb.PathScheme)
}

func testIteratorContinueAfterSeekError(t *testing.T, memonly bool, scheme string) {
	// Commit test trie to db, then remove the node containing "bars".
	var (
		barNodePath []byte
		barNodeHash = common.HexToHash("05041990364eb72fcb1127652ce40d8bab765f2bfe53225b1170d276cc101c2e")
	)
	diskdb := rawdb.NewMemoryDatabase()
	triedb := newTestDatabase(diskdb, scheme)
	ctr := NewEmpty(triedb)
	for _, val := range testdata1 {
		ctr.MustUpdate([]byte(val.k), []byte(val.v))
	}
	root, nodes, _ := ctr.Commit(false)
	for path, n := range nodes.Nodes {
		if n.Hash == barNodeHash {
			barNodePath = []byte(path)
			break
		}
	}
	triedb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil)
	if !memonly {
		triedb.Commit(root, false)
	}
	var (
		barNodeBlob []byte
	)
	tr, _ := New(TrieID(root), triedb)
	if memonly {
		tr.reader.banned = map[string]struct{}{string(barNodePath): {}}
	} else {
		barNodeBlob = rawdb.ReadTrieNode(diskdb, common.Hash{}, barNodePath, barNodeHash, triedb.Scheme())
		rawdb.DeleteTrieNode(diskdb, common.Hash{}, barNodePath, barNodeHash, triedb.Scheme())
	}
	// Create a new iterator that seeks to "bars". Seeking can't proceed because
	// the node is missing.
	it := tr.MustNodeIterator([]byte("bars"))
	missing, ok := it.Error().(*MissingNodeError)
	if !ok {
		t.Fatal("want MissingNodeError, got", it.Error())
	} else if missing.NodeHash != barNodeHash {
		t.Fatal("wrong node missing")
	}
	// Reinsert the missing node.
	if memonly {
		delete(tr.reader.banned, string(barNodePath))
	} else {
		rawdb.WriteTrieNode(diskdb, common.Hash{}, barNodePath, barNodeHash, barNodeBlob, triedb.Scheme())
	}
	// Check that iteration produces the right set of values.
	if err := checkIteratorOrder(testdata1[2:], NewIterator(it)); err != nil {
		t.Fatal(err)
	}
}

func checkIteratorNoDups(t *testing.T, it NodeIterator, seen map[string]bool) int {
	if seen == nil {
		seen = make(map[string]bool)
	}
	for it.Next(true) {
		if seen[string(it.Path())] {
			t.Fatalf("iterator visited node path %x twice", it.Path())
		}
		seen[string(it.Path())] = true
	}
	return len(seen)
}

func TestIteratorNodeBlob(t *testing.T) {
	testIteratorNodeBlob(t, rawdb.HashScheme)
	testIteratorNodeBlob(t, rawdb.PathScheme)
}

func testIteratorNodeBlob(t *testing.T, scheme string) {
	var (
		db     = rawdb.NewMemoryDatabase()
		triedb = newTestDatabase(db, scheme)
		trie   = NewEmpty(triedb)
	)
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
	}
	all := make(map[string]string)
	for _, val := range vals {
		all[val.k] = val.v
		trie.MustUpdate([]byte(val.k), []byte(val.v))
	}
	root, nodes, _ := trie.Commit(false)
	triedb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil)
	triedb.Commit(root, false)

	var found = make(map[common.Hash][]byte)
	trie, _ = New(TrieID(root), triedb)
	it := trie.MustNodeIterator(nil)
	for it.Next(true) {
		if it.Hash() == (common.Hash{}) {
			continue
		}
		found[it.Hash()] = it.NodeBlob()
	}

	dbIter := db.NewIterator(nil, nil)
	defer dbIter.Release()

	var count int
	for dbIter.Next() {
		ok, _, _ := isTrieNode(triedb.Scheme(), dbIter.Key(), dbIter.Value())
		if !ok {
			continue
		}
		got, present := found[crypto.Keccak256Hash(dbIter.Value())]
		if !present {
			t.Fatal("Miss trie node")
		}
		if !bytes.Equal(got, dbIter.Value()) {
			t.Fatalf("Unexpected trie node want %v got %v", dbIter.Value(), got)
		}
		count += 1
	}
	if count != len(found) {
		t.Fatal("Find extra trie node via iterator")
	}
}

// isTrieNode is a helper function which reports if the provided
// database entry belongs to a trie node or not. Note in tests
// only single layer trie is used, namely storage trie is not
// considered at all.
func isTrieNode(scheme string, key, val []byte) (bool, []byte, common.Hash) {
	var (
		path []byte
		hash common.Hash
	)
	if scheme == rawdb.HashScheme {
		ok := rawdb.IsLegacyTrieNode(key, val)
		if !ok {
			return false, nil, common.Hash{}
		}
		hash = common.BytesToHash(key)
	} else {
		ok, remain := rawdb.ResolveAccountTrieNodeKey(key)
		if !ok {
			return false, nil, common.Hash{}
		}
		path = common.CopyBytes(remain)
		hash = crypto.Keccak256Hash(val)
	}
	return true, path, hash
}

func BenchmarkIterator(b *testing.B) {
	diskDb, srcDb, tr, _ := makeTestTrie(rawdb.HashScheme)
	root := tr.Hash()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := checkTrieConsistency(diskDb, srcDb.Scheme(), root, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMerkleTreeIterator(t *testing.T) {
	testIterator := func(t *testing.T, db *memorydb.Database, it NodeIterator) (count int, leafCount int) {
		for ; it.Next(false); count++ {
			db.Delete(zkt.ReverseByteOrder(it.Hash().Bytes()))
			if it.Leaf() {
				leafCount++
				leafPath := zk.NewTreePathFromHashBig(common.BytesToHash(it.LeafKey()))
				if !bytes.Equal(it.Path(), leafPath[:len(it.Path())]) {
					t.Errorf("incorrect tree path. iterator path : %v, leaf path %v", it.Path(), leafPath)
				}
			}
		}
		return
	}

	makeMerkleTreeWithData := func(input []kvs) (*ZkMerkleStateTrie, *memorydb.Database) {
		db := memorydb.New()
		zdb := NewZkDatabase(rawdb.NewDatabase(db))
		tree := NewEmptyZkMerkleStateTrie(zdb)
		for _, val := range input {
			tree.Update(common.LeftPadBytes([]byte(val.k), 32), common.LeftPadBytes([]byte(val.v), 32))
		}
		rootHash, _, _ := tree.Commit(false)
		zdb.Commit(rootHash, true)
		return tree, db
	}

	t.Run("zk merkle tree", func(t *testing.T) {
		tree, db := makeMerkleTreeWithData(testdata1)
		expected := db.Len()
		it, _ := tree.NodeIterator(nil)
		count, leafCount := testIterator(t, db, it)
		if db.Len() != 0 {
			t.Errorf("db is not empty. remain size %d", db.Len())
		}
		if expected != count {
			t.Errorf("iterated node count invalid. expected %d, but got %d", db.Len(), count)
		}
		if leafCount != len(testdata1) {
			t.Errorf("iterated leaf count invalid. expected %d, but got %d", len(testdata1), leafCount)
		}
	})

	t.Run("zk merkle tree with start", func(t *testing.T) {
		tree, _ := makeMerkleTreeWithData(testdata1)
		var inputs []*kvs
		for _, data := range testdata1 {
			hash := zk.MustNewSecureHash(common.LeftPadBytes([]byte(data.k), 32))
			inputs = append(inputs, &kvs{k: string(BytesToZkIteratorKey(hash[:]).Bytes()), v: data.v})
		}
		slices.SortFunc(inputs, func(a, b *kvs) int { return bytes.Compare([]byte(a.k), []byte(b.k)) })
		for idx := 0; idx < len(inputs); idx++ {
			start := idx
			t.Run(fmt.Sprintf("test with start index %d", start), func(t *testing.T) {
				it, _ := tree.NodeIterator([]byte(inputs[start].k))
				count := 0
				for it.Next(true) {
					if it.Leaf() {
						count++
					}
				}
				if start+count != len(inputs) {
					t.Fatalf("incorrect leaf node count, start %d, find %d, total %d", start, count, len(inputs))
				}
			})
		}
	})

	t.Run("zktrie", func(t *testing.T) {
		db := memorydb.New()
		zkdb := NewZkDatabase(rawdb.NewDatabase(db))
		trie, _ := NewZkTrie(common.Hash{}, zkdb)
		for _, val := range testdata1 {
			trie.TryUpdate(common.LeftPadBytes([]byte(val.k), 32), []byte(val.v))
		}
		root, _, _ := trie.Commit(false)
		zkdb.Commit(root, true)

		nodeCount := db.Len()
		it, _ := trie.NodeIterator(nil)
		count, leafCount := testIterator(t, db, it)
		if nodeCount-db.Len() != count {
			t.Errorf("iterated node count invalid. expected %d, but got %d", nodeCount-db.Len(), count)
		}
		if leafCount != len(testdata1) {
			t.Errorf("iterated leaf count invalid. expected %d, but got %d", len(testdata1), leafCount)
		}
	})

	t.Run("single", func(t *testing.T) {
		prepareTest := func() (*ZkTrie, *memorydb.Database, kvs) {
			db := memorydb.New()
			zkdb := NewZkDatabase(rawdb.NewDatabase(db))
			trie, _ := NewZkTrie(common.Hash{}, zkdb)
			input := testdata1[0]
			trie.TryUpdate(common.LeftPadBytes([]byte(input.k), 32), []byte(input.v))

			root, _, _ := trie.Commit(false)
			zkdb.Commit(root, true)
			return trie, db, input
		}

		t.Run("no start", func(t *testing.T) {
			trie, db, _ := prepareTest()
			it := trie.MustNodeIterator(nil)
			count, leafCount := testIterator(t, db, it)
			if count != 1 {
				t.Errorf("iterated node count invalid. expected %d, but got %d", 1, count)
			}
			if leafCount != 1 {
				t.Errorf("iterated leaf count invalid. expected %d, but got %d", 1, leafCount)
			}
		})

		t.Run("start < leaf", func(t *testing.T) {
			trie, db, _ := prepareTest()
			it := trie.MustNodeIterator(common.BigToHash(common.Big2).Bytes())
			count, leafCount := testIterator(t, db, it)
			if count != 1 {
				t.Errorf("iterated node count invalid. expected %d, but got %d", 1, count)
			}
			if leafCount != 1 {
				t.Errorf("iterated leaf count invalid. expected %d, but got %d", 1, leafCount)
			}
		})

		t.Run("start == leaf", func(t *testing.T) {
			trie, db, input := prepareTest()
			treePath := zk.NewTreePathFromZkHash(*zk.MustNewSecureHash(common.LeftPadBytes([]byte(input.k), 32)))
			it := trie.MustNodeIterator(common.BigToHash(treePath.ToBigInt()).Bytes())
			count, leafCount := testIterator(t, db, it)
			if count != 1 {
				t.Errorf("iterated node count invalid. expected %d, but got %d", 1, count)
			}
			if leafCount != 1 {
				t.Errorf("iterated leaf count invalid. expected %d, but got %d", 1, leafCount)
			}
		})

		t.Run("leaf < start", func(t *testing.T) {
			trie, db, _ := prepareTest()
			it := trie.MustNodeIterator(new(big.Int).Sub(math.BigPow(2, 256), big.NewInt(20)).Bytes())
			count, leafCount := testIterator(t, db, it)
			if count != 0 {
				t.Errorf("iterated node count invalid. expected %d, but got %d", 0, count)
			}
			if leafCount != 0 {
				t.Errorf("iterated leaf count invalid. expected %d, but got %d", 0, leafCount)
			}
		})
	})

	t.Run("start param", func(t *testing.T) {
		db := memorydb.New()
		zdb := NewZkDatabase(rawdb.NewDatabase(db))
		tree := NewEmptyZkMerkleStateTrie(zdb)
		tree.MustUpdate(common.BigToHash(new(big.Int).SetInt64(10)).Bytes(), []byte("a"))
		tree.MustUpdate(common.BigToHash(new(big.Int).SetInt64(45)).Bytes(), []byte("b"))

		root, _, _ := tree.Commit(false)
		zdb.Commit(root, true)

		it := tree.MustNodeIterator(new(big.Int).Sub(math.BigPow(2, 256), big.NewInt(20)).Bytes())
		count, leafCount := testIterator(t, db, it)
		if count != 0 {
			t.Errorf("iterated node count invalid. expected %d, but got %d", 0, count)
		}
		if leafCount != 0 {
			t.Errorf("iterated leaf count invalid. expected %d, but got %d", 0, leafCount)
		}
	})

	// 2117868253024658673051308919765768251327889971706193468262358283651495249312
	// 5890867801457181665214479254972498000781191528432013068122435103377766174560
	// 14581765644332007355603742199734881084194908881797547524839575783062290731888
	// 34788104062452934851384508780622905500996384124020206338343429550016154937976
	// 35276556762053997527613056591322528636494104613584587955086221921567012068952
	// 35360899632278431044740218782056701006787372914479814587499001065171395100648
	// 37464597366772015210317945055100241299952644205502865319476387775685341350804
	// 37969619727885826713456741609104273363135728311753922979242122609203067018480
	// 49105898062310628859001440408801641574777586797109612022294302697255598990260
	// 52741712599410822789960629764379820729046689701270413750704650756582052966208
	// 60605793585936038718222944469367330975045253165943558504994436015126663057264
	// 85820665183479862833099943033748518022104258034302536812924589654960629631092
	// 85903213226554376758871722034242209534889372863809613891381864212832081120192
	// 90325294576998166929651976356705470786861187287245759150875554730994004964376
	// 100426110323909178507910617048938140138509083918485403644527157931210594668280
	// 101121973535399381299596552991112472380608726703043018240306005545403524541156
	// 107728238818287576212658976290069527420728087670830176810004760597624147961456
	// 108965031955345082684882074272074576895157534795165059799486221336737027828244
	// 109990652738555876101121666461374586270194093281384129526042736168378582559940
	// 110211460199408811616376306299384222697381109719033443765762911326375974320200
	t.Run("startKey", func(t *testing.T) {
		testLeafCount := func(expectedLeafCount int) func(t *testing.T) {
			return func(t *testing.T) {
				db := memorydb.New()
				zdb := NewZkDatabase(rawdb.NewDatabase(db))
				tree := NewEmptyZkMerkleStateTrie(zdb)

				keys, values := makeTreeInput()
				for i := range keys {
					tree.MustUpdate(keys[i], values[i])
				}
				root, _, _ := tree.Commit(false)
				if err := zdb.Commit(root, true); err != nil {
					t.Error(err)
				}

				var startKey []byte
				if names := strings.Split(t.Name(), "/"); names[len(names)-1] != "null" {
					start, _ := new(big.Int).SetString(names[len(names)-1], 10)
					startKey = common.BigToHash(start).Bytes()
				}
				//tree, _ := NewZkMerkleStateTrie(root, zdb)
				//it := tree.MustNodeIterator(startKey)
				it := tree.MustNodeIterator(startKey)
				if it.Error() != nil {
					t.Error(it.Error())
				}
				_, leafCount := testIterator(t, db, it)
				if leafCount != expectedLeafCount {
					t.Errorf("expected leaf count : %v, but got : %v", expectedLeafCount, leafCount)
				}
			}
		}

		testRangeRandom := func(expectedLeafCount int) func(t *testing.T) {
			return func(t *testing.T) {
				names := strings.Split(t.Name(), "/")
				startRandomRange := strings.Split(names[len(names)-1], "~")
				for i := 0; i < 100; i++ {
					start := randomBigInt(
						startRandomRange[0],
						startRandomRange[1],
					)
					t.Run(start.String(), testLeafCount(expectedLeafCount))
				}
			}
		}

		t.Run("null", testLeafCount(20))
		t.Run("0~2117868253024658673051308919765768251327889971706193468262358283651495249312", testRangeRandom(20))
		t.Run("2117868253024658673051308919765768251327889971706193468262358283651495249311", testLeafCount(20))
		t.Run("2117868253024658673051308919765768251327889971706193468262358283651495249312", testLeafCount(20)) // leaf hit
		t.Run("2117868253024658673051308919765768251327889971706193468262358283651495249313", testLeafCount(19))
		t.Run("2117868253024658673051308919765768251327889971706193468262358283651495249313~5890867801457181665214479254972498000781191528432013068122435103377766174559", testRangeRandom(19))
		t.Run("5890867801457181665214479254972498000781191528432013068122435103377766174559", testLeafCount(19))
		t.Run("5890867801457181665214479254972498000781191528432013068122435103377766174560", testLeafCount(19)) // leaf hit
		t.Run("5890867801457181665214479254972498000781191528432013068122435103377766174561", testLeafCount(18))
		t.Run("14581765644332007355603742199734881084194908881797547524839575783062290731888", testLeafCount(18)) // leaf hit
		t.Run("34788104062452934851384508780622905500996384124020206338343429550016154937976", testLeafCount(17)) // leaf hit
		t.Run("35276556762053997527613056591322528636494104613584587955086221921567012068952", testLeafCount(16)) // leaf hit
		t.Run("35360899632278431044740218782056701006787372914479814587499001065171395100648", testLeafCount(15)) // leaf hit
		t.Run("37464597366772015210317945055100241299952644205502865319476387775685341350804", testLeafCount(14)) // leaf hit
		t.Run("37969619727885826713456741609104273363135728311753922979242122609203067018480", testLeafCount(13)) // leaf hit
		t.Run("37969619727885826713456741609104273363135728311753922979242122609203067018481~49105898062310628859001440408801641574777586797109612022294302697255598990259", testRangeRandom(12))
		t.Run("43059581450286369774720893806032160531653066764655610223233743297647867892559", testLeafCount(12))
		t.Run("49105898062310628859001440408801641574777586797109612022294302697255598990260", testLeafCount(12)) // leaf hit
		t.Run("52741712599410822789960629764379820729046689701270413750704650756582052966208", testLeafCount(11)) // leaf hit
		t.Run("60605793585936038718222944469367330975045253165943558504994436015126663057264", testLeafCount(10)) // leaf hit
		t.Run("85820665183479862833099943033748518022104258034302536812924589654960629631092", testLeafCount(9))  // leaf hit
		t.Run("85903213226554376758871722034242209534889372863809613891381864212832081120192", testLeafCount(8))  // leaf hit
		t.Run("90325294576998166929651976356705470786861187287245759150875554730994004964376", testLeafCount(7))  // leaf hit
		t.Run("90325294576998166929651976356705470786861187287245759150875554730994004964377~100426110323909178507910617048938140138509083918485403644527157931210594668279", testRangeRandom(6))
		t.Run("94522316217657177309342980577705593912865267091010285900227687201142635166164", testLeafCount(6))
		t.Run("100426110323909178507910617048938140138509083918485403644527157931210594668280", testLeafCount(6)) // leaf hit
		t.Run("101121973535399381299596552991112472380608726703043018240306005545403524541156", testLeafCount(5)) // leaf hit
		t.Run("107728238818287576212658976290069527420728087670830176810004760597624147961456", testLeafCount(4)) // leaf hit
		t.Run("108965031955345082684882074272074576895157534795165059799486221336737027828244", testLeafCount(3)) // leaf hit
		t.Run("109990652738555876101121666461374586270194093281384129526042736168378582559940", testLeafCount(2)) // leaf hit
		t.Run("110211460199408811616376306299384222697381109719033443765762911326375974320200", testLeafCount(1)) // leaf hit
		t.Run("110211460199408811616376306299384222697381109719033443765762911326375974320201", testLeafCount(0))
		t.Run("110211460199408811616376306299384222697381109719033443765762911326375974320201~115792089237316195423570985008687907853269984665640564039457584007913129639935", testRangeRandom(0))
		t.Run("115792089237316195423570985008687907853269984665640564039457584007913129639935", testLeafCount(0)) // 2^256-1
	})
}

func makeTreeInput() ([][]byte, [][]byte) {
	r := rand.New(rand.NewSource(0))
	keys := make([][]byte, 20)
	values := make([][]byte, 20)
	for i := range keys {
		keys[i] = make([]byte, 32)
		values[i] = make([]byte, 32)
		r.Read(keys[i])
		r.Read(values[i])
	}
	return keys, values
}

func randomBigInt(start, end string) *big.Int {
	startNum, _ := new(big.Int).SetString(start, 10)
	endNum, _ := new(big.Int).SetString(end, 10)
	rangeNum := new(big.Int).Sub(endNum, startNum)
	randomNum, _ := crand.Int(crand.Reader, rangeNum)
	return new(big.Int).Add(startNum, randomNum)
}
