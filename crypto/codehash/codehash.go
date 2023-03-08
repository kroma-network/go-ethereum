// This is taken from https://github.com/scroll-tech/go-ethereum/blob/staging/crypto/codehash/codehash.go
package codehash

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/poseidon"
)

func CodeHash(code []byte) (h common.Hash) {
	return poseidon.CodeHash(code)
}
