package zk

import (
	"log"
	"math/big"

	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
)

// TreePath 0 left, 1 right
type TreePath []byte

const (
	left  = byte(0)
	right = byte(1)
)

func NewTreePathFromHashBig(hash common.Hash) TreePath {
	return NewTreePathFromBig(hash.Big(), len(hash)*8)
}

func NewTreePathFromBig(key *big.Int, maxLevel int) TreePath {
	result := make([]byte, maxLevel)
	for i := 0; i < maxLevel; i++ {
		bit := new(big.Int).Mod(key, big.NewInt(2))
		if bit.Uint64() == 1 {
			result[maxLevel-1-i] = byte(bit.Int64())
		}
		key.Rsh(key, 1)
	}
	return result
}

func NewTreePathFromHash(hash common.Hash) TreePath {
	return NewTreePathFromBytesAndMaxLevel(zkt.ReverseByteOrder(hash[:]), len(hash)*8)
}

func NewTreePathFromZkHash(hash zkt.Hash) TreePath {
	return NewTreePathFromBytesAndMaxLevel(hash[:], len(hash)*8)
}

func NewTreePathFromBytes(bytes []byte) TreePath {
	return NewTreePathFromBytesAndMaxLevel(bytes, len(bytes)*8)
}

func NewTreePathFromBytesAndMaxLevel(bytes []byte, maxLevel int) TreePath {
	path := make(TreePath, maxLevel)
	for n := 0; n < maxLevel; n++ {
		if zkt.TestBit(bytes[:], uint(n)) {
			path[n] = right
		} else {
			path[n] = left
		}
	}
	return path
}

func NewTreePath(path []byte) TreePath { return path }

func (p TreePath) Get(depth int) byte      { return p[depth] }
func (p TreePath) GetOther(depth int) byte { return p[depth] ^ right }

func (p TreePath) ToHash() *common.Hash {
	hash := common.BytesToHash(p.ToZkHash().Bytes())
	return &hash
}

func (p TreePath) ToZkHash() *zkt.Hash {
	var zkHash zkt.Hash
	copy(zkHash[:], p.toHexBytes())
	return &zkHash
}

func (p TreePath) toHexBytes() []byte {
	bytes := make([]byte, len(p)/8)
	for i := 0; i < len(p); i += 8 {
		var value byte
		for _, path := range zkt.ReverseByteOrder(p[i : i+8]) {
			value = (value << 1) | path
		}
		bytes[i/8] = value
	}
	return bytes
}

func (p TreePath) NextPath() TreePath {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == 0x0 {
			p[i] = 0x1
			break
		} else if p[i] == 0x1 {
			p[i] = 0x0
		} else {
			log.Panicf("invalid tree path %v\n", p)
		}
	}
	return p
}

func (p TreePath) PrevPath() TreePath {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == 0x0 {
			p[i] = 0x1
		} else if p[i] == 0x1 {
			p[i] = 0x0
			break
		} else {
			log.Panicf("invalid tree path %v\n", p)
		}
	}
	return p
}

func (p TreePath) ToBigInt() *big.Int {
	result := new(big.Int)
	lastIdx := len(p) - 1
	for i, b := range p {
		result.SetBit(result, lastIdx-i, uint(b))
	}
	return result
}
