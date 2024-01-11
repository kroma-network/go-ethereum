package zk

import (
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
)

// TreePath 0 left, 1 right
type TreePath []byte

const (
	left  = byte(0)
	right = byte(1)
)

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
