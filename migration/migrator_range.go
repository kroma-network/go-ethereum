package migration

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"runtime"

	"github.com/holiman/uint256"
	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	NumProcessAccount = runtime.NumCPU()
	NumProcessStorage = runtime.NumCPU()
)

var hashSpace = new(big.Int).Exp(common.Big2, common.Big256, nil)

// hashRange is a utility to handle ranges of hashes, Split up the
// hash-space into sections, and 'walk' over the sections
type hashRange struct {
	current *uint256.Int
	step    *uint256.Int
}

// newHashRange creates a new hashRange, initiated at the start position,
// and with the step set to fill the desired 'num' chunks
func newHashRange(start common.Hash, num uint64) *hashRange {
	left := new(big.Int).Sub(hashSpace, start.Big())
	step := new(big.Int).Div(
		new(big.Int).Add(left, new(big.Int).SetUint64(num-1)),
		new(big.Int).SetUint64(num),
	)
	step256 := new(uint256.Int)
	step256.SetFromBig(step)

	return &hashRange{
		current: new(uint256.Int).SetBytes32(start[:]),
		step:    step256,
	}
}

// Next pushes the hash range to the next interval.
func (r *hashRange) Next() bool {
	if r.step.IsZero() {
		return false
	}
	next, overflow := new(uint256.Int).AddOverflow(r.current, r.step)
	if overflow {
		return false
	}
	r.current = next
	return true
}

// Start returns the first hash in the current interval.
func (r *hashRange) Start() common.Hash {
	return r.current.Bytes32()
}

// End returns the last hash in the current interval.
func (r *hashRange) End() common.Hash {
	// If the end overflows (non divisible range), return a shorter interval
	next, overflow := new(uint256.Int).AddOverflow(r.current, r.step)
	if overflow {
		return common.MaxHash
	}
	return next.SubUint64(next, 1).Bytes32()
}

// hashRangeIterator divides the iteration range using trie node hashes and traverses leaf nodes.
// The traversed nodes are returned via the onLeaf callback function.
func hashRangeIterator(ctx context.Context, tr state.Trie, num int, onLeaf func(key, value []byte) error) error {
	r := newHashRange(common.Hash{}, uint64(num))

	eg, cCtx := errgroup.WithContext(ctx)
	for {
		startKey := r.Start().Bytes()
		endKey := r.End().Bytes()
		eg.Go(func() error {
			nodeIt, err := tr.NodeIterator(startKey)
			if err != nil {
				return fmt.Errorf("failed to open node iterator (root: %s): %w", tr.Hash(), err)
			}
			iter := trie.NewIterator(nodeIt)
			for iter.Next() {
				if bytes.Compare(iter.Key, startKey) == -1 {
					continue
				}
				if bytes.Compare(iter.Key, endKey) == 1 {
					break
				}
				err = onLeaf(iter.Key, iter.Value)
				if err != nil {
					return err
				}
				if bytes.Equal(iter.Key, endKey) {
					break
				}
			}
			if iter.Err != nil {
				return fmt.Errorf("failed to traverse state trie (root: %s): %w", tr.Hash(), iter.Err)
			}
			return nil
		})

		if !r.Next() {
			break
		}

		select {
		case <-cCtx.Done():
			return cCtx.Err()
		default:
		}
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}
