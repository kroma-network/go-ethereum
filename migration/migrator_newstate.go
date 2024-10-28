package migration

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

// remove all storage values in the input storage trie
func deleteAccountStorage(mptStorageTrie *trie.StateTrie) error {
	if mptStorageTrie.Hash().Cmp(types.EmptyRootHash) == 0 {
		return nil
	}

	storageIt, err := mptStorageTrie.NodeIterator(nil)
	if err != nil {
		return err
	}
	storageIter := trie.NewIterator(storageIt)
	for storageIter.Next() {
		if err := mptStorageTrie.DeleteStorage(common.Address{}, storageIter.Key); err != nil {
			return err
		}
	}

	if storageIter.Err != nil {
		return storageIter.Err
	}

	return nil
}

func (m *StateMigrator) applyNewStateTransition(headNumber uint64) error {
	start := m.migratedRef.BlockNumber() + 1
	root := m.migratedRef.Root()
	for i := start; i <= headNumber; i++ {
		log.Info("Apply new state to MPT", "block", i, "root", root.TerminalString())

		mptStateTrie, err := trie.NewStateTrie(trie.StateTrieID(root), m.mptdb)
		if err != nil {
			return err
		}

		header := m.backend.BlockChain().GetHeaderByNumber(i)
		if header == nil {
			return fmt.Errorf("block %d header not found", i)
		}

		batch := m.db.NewBatch()
		accountChangesKey := core.AccountChangesKey(i)

		serAccountChanges, err := m.db.Get(accountChangesKey)
		if err != nil {
			return err
		}

		if err := batch.Delete(accountChangesKey); err != nil {
			return err
		}

		accountChanges, err := core.DeserializeStateChanges[map[common.Hash][]byte](serAccountChanges)
		if err != nil {
			return err
		}

		storageChangesKey := core.StorageChangesKey(i)
		serStorageChanges, err := m.db.Get(storageChangesKey)
		if err != nil {
			return err
		}

		if err := batch.Delete(storageChangesKey); err != nil {
			return err
		}

		storageChanges, err := core.DeserializeStateChanges[map[common.Hash]map[common.Hash][]byte](serStorageChanges)
		if err != nil {
			return err
		}

		for hashedAddr, encStateAccount := range accountChanges {
			addr := common.BytesToAddress(m.readZkPreimageWithNonIteratorKey(hashedAddr))

			// if stateAccount is deleted
			if encStateAccount == nil {
				stateAccount, err := mptStateTrie.GetAccount(addr)
				if err != nil {
					return err
				}

				if err := mptStateTrie.DeleteAccount(addr); err != nil {
					return err
				}

				id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), stateAccount.Root)
				mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					return err
				}

				if err := deleteAccountStorage(mptStorageTrie); err != nil {
					return err
				}

				_, err = m.commit(mptStorageTrie, stateAccount.Root)
				if err != nil {
					return err
				}

			} else { // if stateAccount is updated
				// TODO: I'm not sure about the difference between the cases when 'nil' and 'missingTrieNodeErr' are returned
				stateAccount, err := mptStateTrie.GetAccount(addr)
				if err != nil {
					return err
				}

				if stateAccount == nil {
					stateAccount = types.NewEmptyStateAccount(false)
				}

				id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), stateAccount.Root)
				mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					return err
				}

				mptStorageRoot := stateAccount.Root

				destructChangesKey := core.DestructChangesKey(i)
				serDestructChangesKey, err := m.db.Get(destructChangesKey)
				if err != nil {
					return err
				}

				if err := batch.Delete(destructChangesKey); err != nil {
					return err
				}

				destructChanges, err := core.DeserializeStateChanges[map[common.Hash]bool](serDestructChangesKey)
				if err != nil {
					return err
				}

				if destructChanges[hashedAddr] {
					if err := deleteAccountStorage(mptStorageTrie); err != nil {
						return err
					}
				}

				subChanges, exists := storageChanges[hashedAddr]
				if exists {
					for hashedSlotKey, encSlotValue := range subChanges {
						slotKey := m.readZkPreimageWithNonIteratorKey(hashedSlotKey)
						trimmed := common.TrimLeftZeroes(common.BytesToHash(encSlotValue).Bytes())
						if err := mptStorageTrie.UpdateStorage(addr, slotKey, trimmed); err != nil {
							return err
						}
					}
				}

				// it's ok to skip this checking code.
				if destructChanges[hashedAddr] || exists {
					mptStorageRoot, err = m.commit(mptStorageTrie, mptStorageRoot)
					if err != nil {
						return err
					}
				}

				zktStateAccount, err := types.NewStateAccount(encStateAccount, true)
				if err != nil {
					return err
				}

				zktStateAccount.Root = mptStorageRoot

				if err := mptStateTrie.UpdateAccount(addr, zktStateAccount); err != nil {
					return err
				}

			}

		}

		root, err = m.commit(mptStateTrie, root)
		if err != nil {
			return err
		}
		if err := m.migratedRef.Update(root, header.Number.Uint64()); err != nil {
			return err
		}

		if err := batch.Write(); err != nil {
			return err
		}

		batch.Reset()
	}

	return nil
}
