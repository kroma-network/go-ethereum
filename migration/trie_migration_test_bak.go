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
//	"github.com/ethereum/go-ethereum/trie/triedb/hashdb"
//	"github.com/ethereum/go-ethereum/trie/trienode"
//)
//
//func xTestTrieMigration(t *testing.T) {
//	memdb := rawdb.NewMemoryDatabase()
//	mptdb := trie.NewDatabase(memdb, &trie.Config{
//		Preimages: true,
//		HashDB:    hashdb.Defaults,
//	})
//
//	id := trie.StateTrieID(types.EmptyRootHash)
//	mpt, err := trie.NewStateTrie(id, mptdb)
//
//	require.NoError(t, err)
//
//	for i := 0; i < 10000; i++ {
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
//
//	rootNode := set.Nodes[""]
//	fmt.Println("node", "hash", rootNode.Hash, "blob", common.Bytes2Hex(rootNode.Blob))
//	// trie.ForEachBen(rootNode.Blob)
//
//	fmt.Println("state root #1", root)
//
//	id = trie.StateTrieID(root)
//	tempMpt, err := trie.NewStateTrie(id, mptdb)
//	preimage := tempMpt.GetKey(hexutils.HexToBytes("050b07000e08000503080a0c0d0a0b0d060103070305030b000f090d080d0104090f040d0b0a09010e080b0e020e070904060e0400090b0f0d0b0e0608050b0910"))
//	fmt.Println("preimage :", preimage)
//	for i := range set.Leaves {
//		fmt.Printf("parent : %s, Blob : %+x\n", set.Leaves[i].Parent.String(), set.Leaves[i].Blob)
//	}
//
//	trie.ForEachBen(rootNode.Blob, memdb)
//	for path, node := range set.Nodes {
//		fmt.Printf("path : %x, tempData : %+x\n", path, node.Blob)
//		// fmt.Printf("hash: %s path: %s isDeleted: %s\n", node.Hash.Hex(), hexutils.BytesToHex([]byte(path)), strconv.FormatBool(node.IsDeleted()))
//		//
//		tempData, _, err := tempMpt.GetNode(hexToCompact([]byte(path)))
//		require.NoError(t, err)
//		// fmt.Printf("path : %x\n", path)
//		fmt.Printf("path : %x, tempData : %+x\n", path, tempData)
//		//
//
//		fmt.Println("isValueNode :", trie.IsValueNode(node.Blob))
//
//		key := tempMpt.GetLeafNodeHash([]byte(path))
//		if key.Cmp(common.Hash{}) != 0 {
//			// fullpath, err := tempMpt.GetTrie().GetFullPath(hexToCompact([]byte(path)))
//			// require.NoError(t, err)
//
//			fullpath := trie.FullPathBen(rootNode.Blob, memdb, []byte(path))
//			fmt.Printf("BEN fullpath : %x\n", fullpath)
//			preimage2 := mpt.GetKey(fullpath)
//			fmt.Println("ben preimage :", preimage2)
//		}
//
//		_ = node
//	}
//
//	id = trie.StateTrieID(root)
//	mpt, err = trie.NewStateTrie(id, mptdb)
//	require.NoError(t, err)
//
//	err = mpt.DeleteAccount(common.BigToAddress(common.Big1))
//	require.NoError(t, err)
//	root2, set, err := mpt.Commit(true)
//
//	require.NoError(t, err)
//	err = mptdb.Update(root2, root, 0, trienode.NewWithNodeSet(set), nil)
//	require.NoError(t, err)
//	err = mptdb.Commit(root2, true)
//	fmt.Println("=====")
//	for i := range set.Leaves {
//		fmt.Printf("parent : %s, Blob : %+x\n", set.Leaves[i].Parent.String(), set.Leaves[i].Blob)
//	}
//
//	mpt, err = trie.NewStateTrie(id, mptdb)
//	preimage2 := mpt.GetKey(hexutils.HexToBytes("050b07000e08000503080a0c0d0a0b0d060103070305030b000f090d080d0104090f040d0b0a09010e080b0e020e070904060e0400090b0f0d0b0e0608050b0910"))
//	fmt.Println("preimage :", preimage2)
//
//	for path, node := range set.Nodes {
//		// fmt.Printf("path : %x, tempData : %+x\n", path, node.Blob)
//		fmt.Printf("hash: %s path: %s isDeleted: %s\n", node.Hash.Hex(), hexutils.BytesToHex([]byte(path)), strconv.FormatBool(node.IsDeleted()))
//		//if node.IsDeleted() {
//		//
//		//}
//
//		if node.IsDeleted() {
//			key := tempMpt.GetLeafNodeHash([]byte(path))
//			if key.Cmp(common.Hash{}) != 0 {
//				// fullpath, err := tempMpt.GetTrie().GetFullPath(hexToCompact([]byte(path)))
//				// require.NoError(t, err)
//
//				fullpath := trie.FullPathBen(rootNode.Blob, memdb, []byte(path))
//				fmt.Printf("DELETED NODE BEN fullpath : %x\n", fullpath)
//				preimage2 := mpt.GetKey(fullpath)
//				fmt.Println("ben DELETED NODE  preimage :", preimage2)
//			}
//		}
//
//		blob, _, err := mpt.GetNode(hexToCompact(hexToCompact([]byte(path))))
//		require.NoError(t, err)
//		fmt.Printf("  Blob1 : %+x\n", blob)
//		fmt.Printf("  Blob2 : %+x\n", node.Blob)
//		key := mpt.GetLeafNodeHash([]byte(path))
//		fmt.Println("leaf node key : ", key.Hex())
//
//		fmt.Println("hasTerm(hex) : ", hasTerm([]byte(path)))
//
//		if !node.IsDeleted() {
//			if key.Cmp(common.Hash{}) != 0 {
//				// TODO(ben) convert path to key
//				// t := mpt.GetTrie()
//				// trie.ForEachBen(rootNode.Blob, memdb)
//
//				fullpath, err := mpt.GetTrie().GetFullPath([]byte(path))
//				require.NoError(t, err)
//				fmt.Printf("fullpath : %x\n", fullpath)
//
//				preimage := mpt.GetKey(hexutils.HexToBytes("050b07000e08000503080a0c0d0a0b0d060103070305030b000f090d080d0104090f040d0b0a09010e080b0e020e070904060e0400090b0f0d0b0e0608050b0910"))
//				fmt.Println("preimage :", preimage)
//			}
//		}
//		// rootNode := set.Nodes[""]
//		// trie.ForEachBen(rootNode.Blob, memdb)
//
//		//
//		//tempData, _, err := tempMpt.GetNode([]byte(path))
//		//require.NoError(t, err)
//		////// fmt.Printf("path : %x\n", path)
//		//fmt.Printf("path : %x, tempData : %+x\n", path, tempData)
//		//
//
//		if node.IsDeleted() {
//			// 1. fullnode인경우 17번쨰 필드를 조회해야한다. 이 경우 조회하면 hash node 노드이거나 value node이겠지. hash node이면 이걸 조회하면 바로 value node이어야만 내가 찾는 leaf node인거다.
//			// 2.hashnode인 경우,  조회하면 value node이어야만한다.
//			// 3. valuenode인 경우는 존재하지 않는다. (parent에 직접 꽂히기 때문에
//			fmt.Println("isValueNode :", trie.IsValueNode(blob))
//		}
//
//		_ = node
//	}
//}
//
//// path : 01, tempData : f869a03468288056310c82aa4c01a7e12a10f8111a0560e72b700555479031b86c357db846f8448001a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
//// path : 05, tempData : f869a03b70e80538acdabd6137353b0f9d8d149f4dba91e8be2e7946e409bfdbe685b9b846f8448003a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
//// path : 0d, tempData : f869a0352688a8f926c816ca1e079067caba944f158e764817b83fc43594370ca9cf62b846f8448002a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
//// path : , tempData : f87180a0ab8cdb808c8303bb61fb48e276217be9770fa83ecf3f90f2234d558885f5abf1808080a0f384b015d55fb15bee99b3f914d15f916e6208b81a78d7ea6f9d5af2d463547e80808080808080a0979b6099cdcf735a24fe74920cd140f688eb613af5f8a969a4273c5bea0dd967808080
//func hexToCompact(hex []byte) []byte {
//	terminator := byte(0)
//	if hasTerm(hex) {
//		terminator = 1
//		hex = hex[:len(hex)-1]
//	}
//	buf := make([]byte, len(hex)/2+1)
//	buf[0] = terminator << 5 // the flag byte
//	if len(hex)&1 == 1 {
//		buf[0] |= 1 << 4 // odd flag
//		buf[0] |= hex[0] // first nibble is contained in the first byte
//		hex = hex[1:]
//	}
//	decodeNibbles(hex, buf[1:])
//	return buf
//}
//
//// hexToCompactInPlace places the compact key in input buffer, returning the compacted key.
//func hexToCompactInPlace(hex []byte) []byte {
//	var (
//		hexLen    = len(hex) // length of the hex input
//		firstByte = byte(0)
//	)
//	// Check if we have a terminator there
//	if hexLen > 0 && hex[hexLen-1] == 16 {
//		firstByte = 1 << 5
//		hexLen-- // last part was the terminator, ignore that
//	}
//	var (
//		binLen = hexLen/2 + 1
//		ni     = 0 // index in hex
//		bi     = 1 // index in bin (compact)
//	)
//	if hexLen&1 == 1 {
//		firstByte |= 1 << 4 // odd flag
//		firstByte |= hex[0] // first nibble is contained in the first byte
//		ni++
//	}
//	for ; ni < hexLen; bi, ni = bi+1, ni+2 {
//		hex[bi] = hex[ni]<<4 | hex[ni+1]
//	}
//	hex[0] = firstByte
//	return hex[:binLen]
//}
//
//func compactToHex(compact []byte) []byte {
//	if len(compact) == 0 {
//		return compact
//	}
//	base := keybytesToHex(compact)
//	// delete terminator flag
//	if base[0] < 2 {
//		base = base[:len(base)-1]
//	}
//	// apply odd flag
//	chop := 2 - base[0]&1
//	return base[chop:]
//}
//
//func keybytesToHex(str []byte) []byte {
//	l := len(str)*2 + 1
//	nibbles := make([]byte, l)
//	for i, b := range str {
//		nibbles[i*2] = b / 16
//		nibbles[i*2+1] = b % 16
//	}
//	nibbles[l-1] = 16
//	return nibbles
//}
//
//// hexToKeybytes turns hex nibbles into key bytes.
//// This can only be used for keys of even length.
//func hexToKeybytes(hex []byte) []byte {
//	if hasTerm(hex) {
//		hex = hex[:len(hex)-1]
//	}
//	if len(hex)&1 != 0 {
//		panic("can't convert hex key of odd length")
//	}
//	key := make([]byte, len(hex)/2)
//	decodeNibbles(hex, key)
//	return key
//}
//
//func decodeNibbles(nibbles []byte, bytes []byte) {
//	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
//		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
//	}
//}
//
//// prefixLen returns the length of the common prefix of a and b.
//func prefixLen(a, b []byte) int {
//	i, length := 0, len(a)
//	if len(b) < length {
//		length = len(b)
//	}
//	for ; i < length; i++ {
//		if a[i] != b[i] {
//			break
//		}
//	}
//	return i
//}
//
//// hasTerm returns whether a hex key has the terminator flag.
//func hasTerm(s []byte) bool {
//	return len(s) > 0 && s[len(s)-1] == 16
//}
