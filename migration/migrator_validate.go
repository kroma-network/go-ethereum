package migration

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

func (m *StateMigrator) ValidateMigratedState(ctx context.Context, mptRoot common.Hash, zkRoot common.Hash) error {
	var accounts atomic.Uint64
	var slots atomic.Uint64

	eg, cCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		mpt, err := trie.NewStateTrie(trie.StateTrieID(mptRoot), m.mptdb)
		if err != nil {
			log.Error("Failed to create state trie", "root", mptRoot, "err", err)
			return err
		}

		zkt, err := trie.NewZkMerkleStateTrie(zkRoot, m.zktdb)
		if err != nil {
			log.Error("Failed to create zk state trie", "root", zkRoot, "err", err)
			return err
		}

		var mu sync.Mutex
		err = hashRangeIterator(zkt, NumProcessAccount, func(key, value []byte) error {
			accounts.Add(1)
			zkAcc, err := types.NewStateAccount(value, true)
			if err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return err
			}

			preimage, err := m.readZkPreimage(key)
			if err != nil {
				return err
			}
			addr := common.BytesToAddress(preimage)
			mu.Lock()
			mptAcc, err := mpt.GetAccount(addr)
			mu.Unlock()
			if err != nil || mptAcc == nil {
				log.Error("Failed to get account in MPT", "address", addr, "err", err)
				return err
			}
			if mptAcc.Balance.Cmp(zkAcc.Balance) != 0 {
				return fmt.Errorf("account %s balance mismatch. expected %s, got %s", addr, zkAcc.Balance, mptAcc.Balance)
			}
			if mptAcc.Nonce != zkAcc.Nonce {
				return fmt.Errorf("account %s nonce mismatch. expected %d, got %d", addr, zkAcc.Nonce, mptAcc.Nonce)
			}
			if !bytes.Equal(mptAcc.CodeHash, zkAcc.CodeHash) {
				return fmt.Errorf("account %s codehash mismatch. expected %s, got %s", addr, common.BytesToHash(zkAcc.CodeHash), common.BytesToHash(mptAcc.CodeHash))
			}

			if zkAcc.Root != (common.Hash{}) {
				id := trie.StorageTrieID(mptRoot, crypto.Keccak256Hash(addr.Bytes()), mptAcc.Root)
				mptStorage, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					log.Error("Failed to create state trie", "root", mptAcc.Root, "err", err)
					return err
				}

				zktStorage, err := trie.NewZkMerkleStateTrie(zkAcc.Root, m.zktdb)
				if err != nil {
					log.Error("Failed to create zk state trie", "root", zkAcc.Root, "err", err)
					return err
				}
				var mu sync.Mutex
				err = hashRangeIterator(zktStorage, NumProcessStorage, func(key, value []byte) error {
					slots.Add(1)
					slot, err := m.readZkPreimage(key)
					if err != nil {
						return err
					}
					zktVal := common.TrimLeftZeroes(common.BytesToHash(value).Bytes())
					mu.Lock()
					mptVal, err := mptStorage.GetStorage(addr, slot)
					mu.Unlock()
					if err != nil {
						log.Error("Failed to get storage value in MPT", "err", err)
						return err
					}
					if !bytes.Equal(mptVal, zktVal) {
						return fmt.Errorf("account %s storage (slot: %s) mismatch. expected %s, got %s", addr, common.BytesToHash(slot), common.BytesToHash(zktVal), common.BytesToHash(mptVal))
					}
					return nil
				})
				if err != nil {
					return err
				}
			} else if mptAcc.Root != types.EmptyRootHash {
				return fmt.Errorf("account %s root should be empty root hash. got %s", addr, mptAcc.Root)
			}
			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})

	// In progress logger
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				log.Info("Migrated state validation in progress", "accounts", accounts.Load(), "slots", slots.Load())
			case <-cCtx.Done():
				return
			}
		}
	}()

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}
