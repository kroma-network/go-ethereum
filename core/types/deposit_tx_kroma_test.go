package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
)

func TestIsKromaDepositTx(t *testing.T) {
	depTx := NewTx(&DepositTx{
		SourceHash:          common.Hash{31: 1},
		From:                common.Address{0: 1},
		To:                  &common.Address{0: 2},
		Mint:                nil,
		Value:               common.Big0,
		Gas:                 21000,
		IsSystemTransaction: false,
		Data:                nil,
	})
	kromaDepTx := NewTx(&KromaDepositTx{
		SourceHash: common.Hash{31: 1},
		From:       common.Address{0: 1},
		To:         &common.Address{0: 2},
		Mint:       nil,
		Value:      common.Big0,
		Gas:        21000,
		Data:       nil,
	})

	depTxBytes, err := depTx.MarshalBinary()
	require.NoError(t, err)
	require.False(t, IsKromaDepositTx(depTxBytes[1:]))

	kromaDepTxBytes, err := kromaDepTx.MarshalBinary()
	require.NoError(t, err)
	require.True(t, IsKromaDepositTx(kromaDepTxBytes[1:]))

	kromaDepTx = depTx.ToKromaDepositTx()
	kromaDepTxBytes, err = kromaDepTx.MarshalBinary()
	require.NoError(t, err)
	require.True(t, IsKromaDepositTx(kromaDepTxBytes[1:]))
}
