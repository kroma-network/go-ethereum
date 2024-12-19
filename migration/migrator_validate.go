package migration

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"time"

	zktrie "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

func (m *StateMigrator) ValidateStateWithIterator(mptRoot common.Hash, zkRoot common.Hash) error {
	var accounts atomic.Uint64
	var slots atomic.Uint64

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

	nodeIt, err := zkt.NodeIterator(nil)
	if err != nil {
		return fmt.Errorf("failed to open node iterator (root: %s): %w", zkt.Hash(), err)
	}
	iter := trie.NewIterator(nodeIt)
	for iter.Next() {
		zktAcc, err := types.NewStateAccount(iter.Value, true)
		if err != nil {
			log.Error("Invalid account encountered during traversal", "err", err)
			return err
		}

		hk := trie.IteratorKeyToHash(iter.Key, true)
		preimage, err := m.readZkPreimage(*hk)
		if err != nil {
			return err
		}
		addr := common.BytesToAddress(preimage)
		mptAcc, err := mpt.GetAccount(addr)
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

			nodeIt, err := zktStorage.NodeIterator(nil)
			if err != nil {
				return fmt.Errorf("failed to open node iterator (root: %s): %w", zktStorage.Hash(), err)
			}
			iter := trie.NewIterator(nodeIt)
			for iter.Next() {
				hk := trie.IteratorKeyToHash(iter.Key, true)
				preimage, err := m.readZkPreimage(*hk)
				if err != nil {
					return err
				}
				slot := common.BytesToHash(preimage).Bytes()
				zktVal := common.TrimLeftZeroes(common.BytesToHash(iter.Value).Bytes())

				mptVal, err := mptStorage.GetStorage(addr, slot)
				if err != nil {
					log.Error("Failed to get storage value in MPT", "err", err)
					return err
				}
				if !bytes.Equal(mptVal, zktVal) {
					return fmt.Errorf("account %s storage (slot: %s) mismatch. expected %s, got %s", addr, common.BytesToHash(slot), common.BytesToHash(zktVal), common.BytesToHash(mptVal))
				}

				slots.Add(1)
			}
			if iter.Err != nil {
				return fmt.Errorf("failed to traverse state trie (root: %s): %w", zktStorage.Hash(), iter.Err)
			}
		} else if mptAcc.Root != types.EmptyRootHash {
			return fmt.Errorf("account %s root should be empty root hash. got %s", addr, mptAcc.Root)
		}

		accounts.Add(1)
		if err != nil {
			return err
		}
	}
	if iter.Err != nil {
		return fmt.Errorf("failed to traverse state trie (root: %s): %w", zkt.Hash(), iter.Err)
	}

	return nil
}

func (m *StateMigrator) ValidateNewState(num uint64, mptRoot common.Hash, stateChanges *core.StateChanges) error {
	if num == 0 {
		return fmt.Errorf("block number must bigger than 0")
	}
	if mptRoot == types.EmptyRootHash {
		return fmt.Errorf("state root of mpt must be not the empty root hash")
	}
	if stateChanges == nil {
		return fmt.Errorf("state changes set must not be nil")
	}

	parent := m.backend.BlockChain().GetHeaderByNumber(num - 1)
	if parent == nil {
		return fmt.Errorf("failed to get parent header by number %d", num-1)
	}
	header := m.backend.BlockChain().GetHeaderByNumber(num)
	if header == nil {
		return fmt.Errorf("failed to get header by number %d", num)
	}
	zkt, err := trie.NewZkMerkleStateTrie(parent.Root, m.zktdb)
	if err != nil {
		return fmt.Errorf("failed to create zk state trie: %w", err)
	}
	zkt.WithTransformKey(func(key []byte) ([]byte, error) {
		secureKey, err := zktrie.ToSecureKey(key)
		if err != nil {
			return nil, err
		}
		return zktrie.NewHashFromBigInt(secureKey)[:], nil
	})
	mpt, err := trie.NewStateTrie(trie.StateTrieID(mptRoot), m.mptdb)
	if err != nil {
		return fmt.Errorf("fail to create state trie: %w", err)
	}

	addresses := make(map[common.Address]struct{})
	storages := make(map[common.Address][]common.Hash)

	// Collect all addresses and slots
	for addr := range stateChanges.Destruct {
		addresses[addr] = struct{}{}
	}
	for hk := range stateChanges.Accounts {
		preimage, err := m.readZkPreimage(hk)
		if err != nil {
			return fmt.Errorf("failed to read account key preimage(hash: %s): %w", hk, err)
		}
		addr := common.BytesToAddress(preimage)
		addresses[addr] = struct{}{}

		if storageChanges, ok := stateChanges.Storages[hk]; ok {
			for hk := range storageChanges {
				preimage, err := m.readZkPreimage(hk)
				if err != nil {
					return fmt.Errorf("failed to read slot key preimage. address: %s, hash: %s, err: %w", addr, hk, err)
				}
				slot := common.BytesToHash(preimage)
				storages[addr] = append(storages[addr], slot)
			}
		}
	}

	// Begin the validation process with the collected addresses and storage slots.
	for addr := range addresses {
		mptAcc, err := mpt.GetAccount(addr)
		if err != nil {
			return fmt.Errorf("failed to get mpt account %s: %w", addr, err)
		}

		if mptAcc == nil {
			// If the MPT account does not exist, it is also deleted from ZKT.
			err = zkt.DeleteAccount(addr)
			if err != nil {
				return fmt.Errorf("failed to delete zkt account %s: %w", addr, err)
			}
		} else {
			zktAcc, err := zkt.GetAccount(addr)
			if err != nil {
				return fmt.Errorf("failed to get zkt account %s: %w", addr, err)
			}
			// If it is a new account, create and use an empty account.
			if zktAcc == nil {
				zktAcc = types.NewEmptyStateAccount(true)
			}
			zktAcc.Balance = mptAcc.Balance
			zktAcc.Nonce = mptAcc.Nonce
			zktAcc.CodeHash = mptAcc.CodeHash

			// If there are changes to the storage of the account, they are applied to ZKT as well.
			if slots, ok := storages[addr]; ok {
				id := trie.StorageTrieID(mptRoot, crypto.Keccak256Hash(addr.Bytes()), mptAcc.Root)
				mptStorage, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					return fmt.Errorf("failed to create mpt storage: %w", err)
				}
				zktStorage, err := trie.NewZkMerkleStateTrie(zktAcc.Root, m.zktdb)
				if err != nil {
					return fmt.Errorf("failed to create zkt storage: %w", err)
				}
				zktStorage.WithTransformKey(func(key []byte) ([]byte, error) {
					secureKey, err := zktrie.ToSecureKey(key)
					if err != nil {
						return nil, err
					}
					return zktrie.NewHashFromBigInt(secureKey)[:], nil
				})

				for _, slot := range slots {
					val, err := mptStorage.GetStorage(addr, slot.Bytes())
					if err != nil {
						return fmt.Errorf("failed to get mpt storage value. address: %s, slot: %s, err: %w", addr, slot, err)
					}

					if val == nil {
						err = zktStorage.DeleteStorage(addr, slot.Bytes())
						if err != nil {
							return fmt.Errorf("failed to delete zkt storage value. address: %s, slot: %s, err: %w", addr, slot, err)
						}
					} else {
						err = zktStorage.UpdateStorage(addr, slot.Bytes(), val)
						if err != nil {
							return fmt.Errorf("failed to update zkt storage value. address: %s, slot: %s, err: %w", addr, slot, err)
						}
					}
				}

				zktAcc.Root = common.BytesToHash(zktStorage.MerkleTree.Hash())
			}

			err = zkt.UpdateAccount(addr, zktAcc)
			if err != nil {
				return fmt.Errorf("failed to update zkt account. address: %s, err: %w", addr, err)
			}
		}
	}
	root := common.BytesToHash(zkt.MerkleTree.Hash())
	if header.Root != root {
		return fmt.Errorf("failed to validate new state. expected root hash is %s, but got %s", header.Root, root)
	}
	return nil
}
