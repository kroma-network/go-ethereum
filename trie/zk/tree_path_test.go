package zk

import (
	"bytes"
	"math/rand"
	"testing"

	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/crypto/poseidon"
)

func TestNewTreePathFromZkHash(t *testing.T) {
	zkt.InitHashScheme(poseidon.HashFixed)
	rand := rand.New(rand.NewSource(1))
	for i := 0; i < 10000; i++ {
		k, _ := zkt.ToSecureKey([]byte(randomString(rand, 32)))
		input := zkt.NewHashFromBigInt(k)

		path := NewTreePathFromZkHash(*input)

		if !bytes.Equal(input[:], path.ToZkHash()[:]) {
			t.Fatalf("test fail")
		}
	}
}
