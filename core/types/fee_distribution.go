// Copyright 2023 The go-ethereum Authors
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

package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

var (
	ValidatorRewardScalarSlot = common.BigToHash(big.NewInt(7))
)

// FeeDistributionFunc is used in the state transition to determine the validation reward and protocol fee.
// Returns nil if not Kroma execution engine.
type FeeDistributionFunc func(blockNum, gasUsed uint64, baseFee, effectiveTip *big.Int) *FeeDistribution

type FeeDistribution struct {
	Reward   *big.Int
	Protocol *big.Int
}

func NewFeeDistributionFunc(config *params.ChainConfig, statedb StateGetter) FeeDistributionFunc {
	cacheBlockNum := ^uint64(0)
	var scalar uint64
	return func(blockNum, gasUsed uint64, baseFee, effectiveTip *big.Int) *FeeDistribution {
		if config.Kroma == nil {
			return nil
		}

		if blockNum != cacheBlockNum {
			scalar = statedb.GetState(L1BlockAddr, ValidatorRewardScalarSlot).Big().Uint64()
			if scalar > 10000 {
				scalar = 0
			}

			cacheBlockNum = blockNum
		}
		fee := new(big.Int)
		fee.Mul(new(big.Int).SetUint64(gasUsed), baseFee)
		fee.Add(fee, new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), effectiveTip))

		R := big.NewRat(int64(scalar), 10000)
		reward := new(big.Int).Mul(fee, R.Num())
		reward.Div(reward, R.Denom())

		return &FeeDistribution{
			Reward:   reward,
			Protocol: new(big.Int).Sub(fee, reward),
		}
	}
}
