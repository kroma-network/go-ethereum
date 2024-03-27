package misc

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/params"
)

func TestEnsureMulticall3(t *testing.T) {
	ecotoneTime := uint64(1000)
	var tests = []struct {
		name       string
		override   func(cfg *params.ChainConfig)
		timestamp  uint64
		codeExists bool
		applied    bool
	}{
		{
			name:      "at hardfork",
			timestamp: ecotoneTime,
			applied:   true,
		},
		{
			name: "another chain ID",
			override: func(cfg *params.ChainConfig) {
				cfg.ChainID = big.NewInt(params.KromaMainnetChainID)
			},
			timestamp: ecotoneTime,
			applied:   true,
		},
		{
			name:       "code already exists",
			timestamp:  ecotoneTime,
			codeExists: true,
			applied:    true,
		},
		{
			name:      "pre ecotone",
			timestamp: ecotoneTime - 1,
			applied:   false,
		},
		{
			name:      "post hardfork",
			timestamp: ecotoneTime + 1,
			applied:   false,
		},
		{
			name: "ecotone not configured",
			override: func(cfg *params.ChainConfig) {
				cfg.EcotoneTime = nil
			},
			timestamp: ecotoneTime,
			applied:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := params.ChainConfig{
				ChainID:     big.NewInt(params.KromaMainnetChainID),
				Kroma:       &params.KromaConfig{},
				EcotoneTime: &ecotoneTime,
			}
			if tt.override != nil {
				tt.override(&cfg)
			}
			state := &stateDb{
				codeExists: tt.codeExists,
			}
			EnsureMulticall3(&cfg, tt.timestamp, state)
			assert.Equal(t, tt.applied, state.codeSet)
		})
	}
}
