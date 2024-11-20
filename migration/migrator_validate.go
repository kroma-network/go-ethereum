package migration

import (
	"bytes"
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

func (m *StateMigrator) ValidateMigratedState(mptRoot common.Hash, zkRoot common.Hash) error {
	var accounts atomic.Uint64
	var slots atomic.Uint64

	eg, cCtx := errgroup.WithContext(m.ctx)
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
		err = hashRangeIterator(cCtx, zkt, NumProcessAccount, func(key, value []byte) error {
			zktAcc, err := types.NewStateAccount(value, true)
			if err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return err
			}

			hk := trie.IteratorKeyToHash(key, true)
			preimage, err := m.readZkPreimage(*hk)
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
			if mptAcc.Balance.Cmp(zktAcc.Balance) != 0 {
				return fmt.Errorf("account %s balance mismatch. expected %s, got %s", addr, zktAcc.Balance, mptAcc.Balance)
			}
			if mptAcc.Nonce != zktAcc.Nonce {
				return fmt.Errorf("account %s nonce mismatch. expected %d, got %d", addr, zktAcc.Nonce, mptAcc.Nonce)
			}
			if !bytes.Equal(mptAcc.CodeHash, zktAcc.CodeHash) {
				return fmt.Errorf("account %s codehash mismatch. expected %s, got %s", addr, common.BytesToHash(zktAcc.CodeHash), common.BytesToHash(mptAcc.CodeHash))
			}

			if zktAcc.Root != (common.Hash{}) {
				id := trie.StorageTrieID(mptRoot, crypto.Keccak256Hash(addr.Bytes()), mptAcc.Root)
				mptStorage, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					log.Error("Failed to create state trie", "root", mptAcc.Root, "err", err)
					return err
				}

				zktStorage, err := trie.NewZkMerkleStateTrie(zktAcc.Root, m.zktdb)
				if err != nil {
					log.Error("Failed to create zk state trie", "root", zktAcc.Root, "err", err)
					return err
				}
				var mu sync.Mutex
				err = hashRangeIterator(cCtx, zktStorage, NumProcessStorage, func(key, value []byte) error {
					hk := trie.IteratorKeyToHash(key, true)
					preimage, err := m.readZkPreimage(*hk)
					if err != nil {
						return err
					}
					slot := common.BytesToHash(preimage).Bytes()
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

					slots.Add(1)
					return nil
				})
				if err != nil {
					return err
				}
			} else if mptAcc.Root != types.EmptyRootHash {
				return fmt.Errorf("account %s root should be empty root hash. got %s", addr, mptAcc.Root)
			}

			accounts.Add(1)
			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})

	// In progress logger
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	go func() {
		for ; ; <-ticker.C {
			select {
			case <-m.ctx.Done():
				return
			default:
				log.Info("Migrated state validation in progress", "accounts", accounts.Load(), "slots", slots.Load())
			}
		}
	}()

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}
