package migration

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

func (m *StateMigrator) ValidateMigratedState(mptRoot common.Hash, zkRoot common.Hash) error {
	var accounts uint

	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(func() error {
		mpt, err := trie.NewStateTrie(trie.StateTrieID(mptRoot), m.mptdb)
		if err != nil {
			log.Error("Failed to create state trie", "root", mptRoot, "err", err)
			return nil
		}

		acctIt, err := openZkNodeIterator(m.zkdb, zkRoot)
		if err != nil {
			return err
		}
		accIter := trie.NewIterator(acctIt)

		for accIter.Next() {
			accounts += 1
			zkAcc, err := types.NewStateAccount(accIter.Value, true)
			if err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return err
			}

			addr := common.BytesToAddress(m.readZkPreimage(accIter.Key))
			mptAcc, err := mpt.GetAccount(addr)
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
				mpt, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					log.Error("Failed to create state trie", "root", mptAcc.Root, "err", err)
					return err
				}

				storageIt, err := openZkNodeIterator(m.zkdb, zkAcc.Root)
				if err != nil {
					log.Error("Failed to open storage iterator", "root", zkAcc.Root, "err", err)
					return err
				}
				storageIter := trie.NewIterator(storageIt)
				for storageIter.Next() {
					slot := m.readZkPreimage(storageIter.Key)
					zktVal := common.TrimLeftZeroes(common.BytesToHash(storageIter.Value).Bytes())
					mptVal, err := mpt.GetStorage(addr, slot)
					if err != nil {
						log.Error("Failed to get storage value in MPT", "err", err)
						return err
					}
					if !bytes.Equal(mptVal, zktVal) {
						return fmt.Errorf("account %s storage (slot: %s) mismatch. expected %s, got %s", addr, common.Bytes2Hex(slot), common.Bytes2Hex(zktVal), common.Bytes2Hex(mptVal))
					}
				}
			} else if mptAcc.Root != types.EmptyRootHash {
				return fmt.Errorf("account %s root should be empty root hash. got %s", addr, mptAcc.Root)
			}
		}
		return nil
	})

	// In progress logger
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				log.Info("Migrated state validation in progress", "accounts", accounts)
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}
