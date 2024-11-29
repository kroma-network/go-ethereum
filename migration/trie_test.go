package migration

//
//import (
//	"fmt"
//	"math/big"
//	"strconv"
//	"testing"
//
//	"github.com/status-im/keycard-go/hexutils"
//	"github.com/stretchr/testify/require"
//
//	"github.com/ethereum/go-ethereum/common"
//	"github.com/ethereum/go-ethereum/core/rawdb"
//	"github.com/ethereum/go-ethereum/core/types"
//	"github.com/ethereum/go-ethereum/trie"
//	"github.com/ethereum/go-ethereum/trie/trienode"
//)
//
////func decodeNodeUnsafe(hash, buf []byte) error {
////	if len(buf) == 0 {
////		return io.ErrUnexpectedEOF
////	}
////	elems, _, err := rlp.SplitList(buf)
////	if err != nil {
////		return fmt.Errorf("decode error: %v", err)
////	}
////	switch c, _ := rlp.CountValues(elems); c {
////	case 2:
////		n, err := decodeShort(hash, elems)
////		return err
////	case 17:
////		n, err := decodeFull(hash, elems)
////		return err
////	default:
////		return fmt.Errorf("decode error")
////	}
////}
//
//func TestTrie(t *testing.T) {
//	memdb := rawdb.NewMemoryDatabase()
//	mptdb := trie.NewDatabase(memdb, nil)
//
//	id := trie.StateTrieID(types.EmptyRootHash)
//	mpt, err := trie.NewStateTrie(id, mptdb)
//
//	require.NoError(t, err)
//
//	for i := 0; i < 10; i++ {
//		acc := types.NewEmptyStateAccount(false)
//		ii := new(big.Int).SetUint64(uint64(i))
//		acc.Balance = new(big.Int).Add(common.Big1, ii)
//		err = mpt.UpdateAccount(common.BigToAddress(acc.Balance), acc)
//		require.NoError(t, err)
//	}
//
//	root, set, err := mpt.Commit(true)
//	require.NoError(t, err)
//
//	err = mptdb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(set), nil)
//	require.NoError(t, err)
//	err = mptdb.Commit(root, true)
//	require.NoError(t, err)
//	//newMpt, err := trie.NewStateTrie(trie.StateTrieID(root), mptdb)
//	//newMpt, err := trie.New(trie.StateTrieID(root), mptdb)
//	//
//
//	id = trie.StateTrieID(root)
//	mpt2, err := trie.NewStateTrie(id, mptdb)
//
//	err = mpt2.DeleteAccount(common.BigToAddress(common.Big1))
//	require.NoError(t, err)
//
//	root2, set2, err := mpt2.Commit(true)
//	require.NoError(t, err)
//
//	err = mptdb.Update(root2, root, 0, trienode.NewWithNodeSet(set2), nil)
//	require.NoError(t, err)
//	err = mptdb.Commit(root2, true)
//	require.NoError(t, err)
//
//	rootNode := set.Nodes[""]
//	fmt.Println("node", "hash", rootNode.Hash, "blob", common.Bytes2Hex(rootNode.Blob))
//	// trie.ForEachBen(rootNode.Blob)
//
//	fmt.Println("state root #1", root)
//
//	trie.ForEachBen(rootNode.Blob, memdb)
//	id = trie.StateTrieID(root)
//
//	tempMpt, err := trie.NewStateTrie(id, mptdb)
//	for path, node := range set.Nodes {
//		// fmt.Println("node", "path", hexutils.BytesToHex([]byte(path)), "hash", node.Hash, "blob", node.Blob)
//		// key := mpt.GetKey(node.Hash.Bytes())
//		// fmt.Println(hexutils.BytesToHex([]byte(path)))
//		// fmt.Println("node", "hash", node.Hash, "blob", common.Bytes2Hex(node.Blob))
//
//		fmt.Printf("hash: %s path: %s isDeleted: %s\n", node.Hash.Hex(), hexutils.BytesToHex([]byte(path)), strconv.FormatBool(node.IsDeleted()))
//		tempData, _, err := tempMpt.GetNode([]byte(path))
//		require.NoError(t, err)
//		fmt.Printf("path : %x len() : %d\n", path, len(path))
//		hexes := compactToHex([]byte(path))
//		fmt.Printf("compactToHex([]byte(path)) : %x  len() : %d\n", hexes, len(hexes))
//		fmt.Printf("hexToCompact([]byte(path))([]byte(path)) : %x  len() : %d\n", hexToCompact([]byte(path)), len(hexToCompact([]byte(path))))
//
//		fmt.Printf("path : %x, tempData : %+x\n", hexToCompact(hexutils.HexToBytes("01")), tempData)
//
//		// data, resolved, err := newMpt.GetNode([]byte(path))
//		// require.NoError(t, err)
//		// fmt.Println(path, data, resolved)
//
//		// data, err := memdb.Get(node.Hash.Bytes())
//		_ = node
//		// require.NoError(t, err)
//		//if err != nil {
//		//	fmt.Println(err.Error())
//		//} else {
//		//fmt.Println("isTerm : ", hexToKeybytes([]byte(path)))
//		//}
//	}
//
//	fmt.Println("========")
//	/*
//		for path, node := range set2.Nodes {
//			// fmt.Println("node", "path", hexutils.BytesToHex([]byte(path)), "hash", node.Hash, "blob", node.Blob)
//			// key := mpt.GetKey(node.Hash.Bytes())
//			// fmt.Println(hexutils.BytesToHex([]byte(path)))
//			// fmt.Println("node", "hash", node.Hash, "blob", common.Bytes2Hex(node.Blob))
//
//			fmt.Printf("hash: %s path: %s isDeleted: %s\n", node.Hash.Hex(), hexutils.BytesToHex([]byte(path)), strconv.FormatBool(node.IsDeleted()))
//			// data, resolved, err := newMpt.GetNode([]byte(path))
//			// require.NoError(t, err)
//			// fmt.Println(path, data, resolved)
//
//			if node.IsDeleted() {
//				mptTemp, err := trie.NewStateTrie(id, mptdb)
//
//				tempData, _, err := mptTemp.GetNode([]byte(path))
//				require.NoError(t, err)
//				fmt.Printf("path : %x\n", path)
//				fmt.Printf("path : %x, tempData : %+x\n", hexToCompact(hexutils.HexToBytes("01")), tempData)
//
//			}
//			// data, err := memdb.Get(node.Hash.Bytes())
//			_ = node
//			// require.NoError(t, err)
//			//if err != nil {
//			//	fmt.Println(err.Error())
//			//} else {
//			//fmt.Println("isTerm : ", hexToKeybytes([]byte(path)))
//			//}
//		}
//		rootNode2 := set2.Nodes[""]
//
//		trie.ForEachBen(rootNode2.Blob, memdb)
//
//	*/
//}
//
////for i, leaf := range set.Leaves {
////	fmt.Println("leaf", i, "parent", leaf.Parent, "blob", common.Bytes2Hex(leaf.Blob))
////}
//
////for i := 0; i < 10; i++ {
////	ii := new(big.Int).SetUint64(uint64(i))
////	addr := new(big.Int).Add(common.Big1, ii)
////
////	result, err := newMpt.GetAccount(common.BigToAddress(addr))
////	require.NoError(t, err)
////	fmt.Println("result.Balance.String() :", result.Balance.String())
////}
////newMpt.MustGet()
////for it := newMpt.MustNodeIterator(nil); it.Next(true); {
////	if it.Hash() != (common.Hash{}) {
////		fmt.Println("Path :", it.Path(), "IsLeaf:", it.Leaf(), "Hash :", it.Hash(), "Blob : ", it.NodeBlob())
////		//elements[it.Hash()] = iterationElement{
////		//	hash: it.Hash(),
////		//	path: common.CopyBytes(it.Path()),
////		//	blob: common.CopyBytes(it.NodeBlob()),
////		//}
////	}
////}
////
////	for path, node := range set.Nodes {
////		// fmt.Println("node", "path", hexutils.BytesToHex([]byte(path)), "hash", node.Hash, "blob", node.Blob)
////		// key := mpt.GetKey(node.Hash.Bytes())
////		// fmt.Println(hexutils.BytesToHex([]byte(path)))
////		// fmt.Println("node", "hash", node.Hash, "blob", common.Bytes2Hex(node.Blob))
////
////		fmt.Printf("hash: %s path: %s isDeleted: %s\n", node.Hash.Hex(), path, strconv.FormatBool(node.IsDeleted()))
////		// data, resolved, err := newMpt.GetNode([]byte(path))
////		// require.NoError(t, err)
////		// fmt.Println(path, data, resolved)
////
////		// data, err := memdb.Get(node.Hash.Bytes())
////		_ = node
////		// require.NoError(t, err)
////		//if err != nil {
////		//	fmt.Println(err.Error())
////		//} else {
////		//fmt.Println("isTerm : ", hexToKeybytes([]byte(path)))
////		//}
////	}
////
////	//id = trie.StateTrieID(root)
////	//mpt, err = trie.NewStateTrie(id, mptdb)
////	//require.NoError(t, err)
////	//
////	//err = mpt.DeleteAccount(common.BigToAddress(common.Big1))
////	//require.NoError(t, err)
////	//err = mpt.DeleteAccount(common.BigToAddress(common.Big2))
////	//require.NoError(t, err)
////	//root2, set, err := mpt.Commit(true)
////	//require.NoError(t, err)
////	//err = mptdb.Update(root2, root, 0, trienode.NewWithNodeSet(set), nil)
////	//require.NoError(t, err)
////	//err = mptdb.Commit(types.EmptyRootHash, true)
////	//require.NoError(t, err)
////	//_ = root2
////	//fmt.Printf("\n\n")
////	//for i, leaf := range set.Leaves {
////	//	fmt.Println("leaf", i, "parent", leaf.Parent, "blob", common.Bytes2Hex(leaf.Blob))
////	//}
////
////	//root2, set, err := mpt.Commit(true)
////	//require.NoError(t, err)
////	////_ = root2
////	////trie.ForEachBen(set)
////	//
////	//fmt.Println("state root #2", root2)
////
////	//for path, node := range set.Nodes {
////	//	fmt.Println("node", "path", hexutils.BytesToHex([]byte(path)), "hash", node.Hash, "blob", common.Bytes2Hex(node.Blob))
////	//}
////	//
////	//rootNode := set.Nodes[""]
////	//fmt.Println("node", "hash", rootNode.Hash, "blob", common.Bytes2Hex(rootNode.Blob))
////	//trie.ForEachBen(rootNode.Blob)
////
////	//
////}
////
////func hexToCompact(hex []byte) []byte {
////	terminator := byte(0)
////	if hasTerm(hex) {
////		terminator = 1
////		hex = hex[:len(hex)-1]
////	}
////	buf := make([]byte, len(hex)/2+1)
////	buf[0] = terminator << 5 // the flag byte
////	if len(hex)&1 == 1 {
////		buf[0] |= 1 << 4 // odd flag
////		buf[0] |= hex[0] // first nibble is contained in the first byte
////		hex = hex[1:]
////	}
////	decodeNibbles(hex, buf[1:])
////	return buf
////}
////
////// hexToCompactInPlace places the compact key in input buffer, returning the compacted key.
////func hexToCompactInPlace(hex []byte) []byte {
////	var (
////		hexLen    = len(hex) // length of the hex input
////		firstByte = byte(0)
////	)
////	// Check if we have a terminator there
////	if hexLen > 0 && hex[hexLen-1] == 16 {
////		firstByte = 1 << 5
////		hexLen-- // last part was the terminator, ignore that
////	}
////	var (
////		binLen = hexLen/2 + 1
////		ni     = 0 // index in hex
////		bi     = 1 // index in bin (compact)
////	)
////	if hexLen&1 == 1 {
////		firstByte |= 1 << 4 // odd flag
////		firstByte |= hex[0] // first nibble is contained in the first byte
////		ni++
////	}
////	for ; ni < hexLen; bi, ni = bi+1, ni+2 {
////		hex[bi] = hex[ni]<<4 | hex[ni+1]
////	}
////	hex[0] = firstByte
////	return hex[:binLen]
////}
////
////func compactToHex(compact []byte) []byte {
////	if len(compact) == 0 {
////		return compact
////	}
////	base := keybytesToHex(compact)
////	// delete terminator flag
////	if base[0] < 2 {
////		base = base[:len(base)-1]
////	}
////	// apply odd flag
////	chop := 2 - base[0]&1
////	return base[chop:]
////}
////
////func keybytesToHex(str []byte) []byte {
////	l := len(str)*2 + 1
////	nibbles := make([]byte, l)
////	for i, b := range str {
////		nibbles[i*2] = b / 16
////		nibbles[i*2+1] = b % 16
////	}
////	nibbles[l-1] = 16
////	return nibbles
////}
////
////// hexToKeybytes turns hex nibbles into key bytes.
////// This can only be used for keys of even length.
////func hexToKeybytes(hex []byte) []byte {
////	if hasTerm(hex) {
////		hex = hex[:len(hex)-1]
////	}
////	if len(hex)&1 != 0 {
////		panic("can't convert hex key of odd length")
////	}
////	key := make([]byte, len(hex)/2)
////	decodeNibbles(hex, key)
////	return key
////}
////
////func decodeNibbles(nibbles []byte, bytes []byte) {
////	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
////		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
////	}
////}
////
////// prefixLen returns the length of the common prefix of a and b.
////func prefixLen(a, b []byte) int {
////	i, length := 0, len(a)
////	if len(b) < length {
////		length = len(b)
////	}
////	for ; i < length; i++ {
////		if a[i] != b[i] {
////			break
////		}
////	}
////	return i
////}
////
////// hasTerm returns whether a hex key has the terminator flag.
////func hasTerm(s []byte) bool {
////	return len(s) > 0 && s[len(s)-1] == 16
////}
