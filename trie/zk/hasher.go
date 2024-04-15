package zk

import (
	"math/big"

	zkt "github.com/kroma-network/zktrie/types"

	"golang.org/x/crypto/sha3"
)

// Hasher is used to hash the TreeNode.
// For production, e2e, and most testing, poseidonHasher is used, but we've interfaced it this way nonetheless for the following reasons.
//  1. bad performance: poseidon hash is about 100x slower than keccak.
//     So tests with a lot of inputs (1000+) will be too slow to run.
//  2. unhashable : beyond a certain number, poseidon hash throws an error.
//     In many cases, the hash function INPUT is not controllable in the test, and geth does not have any code for when the hash fails.
//
// I thought it doesn't matter whether the hash function is poseidon or keccak for geth,
// and it's very difficult to write tests for the above reasons,
// so I changed it so that you can choose the hash function.
type Hasher interface {
	HashElems(fst, snd *big.Int, elems ...*big.Int) (*zkt.Hash, error)
	PreHandlingElems(flagArray uint32, elems []zkt.Byte32) (*zkt.Hash, error)
}

func NewHasher() Hasher { return NewPoseidonHasher() }

type poseidonHasher struct{}

func NewPoseidonHasher() Hasher { return &poseidonHasher{} }

func (p poseidonHasher) HashElems(fst, snd *big.Int, elems ...*big.Int) (*zkt.Hash, error) {
	return zkt.HashElems(fst, snd, elems...)
}

func (p poseidonHasher) PreHandlingElems(flagArray uint32, elems []zkt.Byte32) (*zkt.Hash, error) {
	return zkt.PreHandlingElems(flagArray, elems)
}

type keccakHasher struct{}

func NewKeccakHasher() Hasher { return &keccakHasher{} }

func (k keccakHasher) HashElems(fst, snd *big.Int, elems ...*big.Int) (*zkt.Hash, error) {
	data := [][]byte{fst.Bytes(), snd.Bytes()}
	for _, elem := range elems {
		data = append(data, elem.Bytes())
	}
	d := sha3.NewLegacyKeccak256()
	for _, b := range data {
		d.Write(b)
	}
	return zkt.NewHashFromBytes(d.Sum(nil)), nil
}

func (k keccakHasher) PreHandlingElems(flagArray uint32, elems []zkt.Byte32) (*zkt.Hash, error) {
	fst := new(big.Int).SetUint64(uint64(flagArray))
	snd := new(big.Int).SetBytes(elems[0].Bytes())
	var bigs []*big.Int
	for _, elem := range elems[1:] {
		bigs = append(bigs, new(big.Int).SetBytes(elem.Bytes()))
	}
	return k.HashElems(fst, snd, bigs...)
}
