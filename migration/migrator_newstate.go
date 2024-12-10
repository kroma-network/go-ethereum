package migration

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// deleteAccountStorage removes all storage values in the input storage trie
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

func (m *StateMigrator) applyDestructChanges(mptStateTrie *trie.StateTrie, root common.Hash, destructChanges map[common.Address]bool) error {
	for addr := range destructChanges {
		acc, err := mptStateTrie.GetAccount(addr)
		if err != nil {
			return err
		}
		if acc == nil {
			continue
		}

		if err := mptStateTrie.DeleteAccount(addr); err != nil {
			return err
		}

		id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), acc.Root)
		mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
		if err != nil {
			return err
		}

		if err := deleteAccountStorage(mptStorageTrie); err != nil {
			return err
		}
	}
	return nil
}

func (m *StateMigrator) applyAccountChanges(tr *trie.StateTrie, bn uint64, root common.Hash, accountChanges map[common.Hash][]byte, storageChanges map[common.Hash]map[common.Hash][]byte) error {
	for hk, val := range accountChanges {
		preimage, err := m.readZkPreimage(hk)
		if err != nil {
			return err
		}
		addr := common.BytesToAddress(preimage)
		acc, err := types.NewStateAccount(val, true)
		if err != nil {
			return err
		}

		mptAcc, err := tr.GetAccount(addr)
		if err != nil {
			return err
		}
		if mptAcc != nil {
			acc.Root = mptAcc.Root
		} else {
			acc.Root = types.EmptyRootHash
		}

		if changes, ok := storageChanges[hk]; ok {
			id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), acc.Root)
			storageTr, err := trie.NewStateTrie(id, m.mptdb)
			if err != nil {
				return err
			}

			var set *trienode.NodeSet
			acc.Root, set, err = m.applyStorageChanges(storageTr, acc.Root, changes)
			if err != nil {
				return err
			}
			// if set is nil, it means there are no changes, so we skip verification in that case.
			if set != nil {
				if err := m.validateStorage(storageTr, id, addr, set, bn); err != nil {
					return err
				}
			}
		}

		if err := tr.UpdateAccount(addr, acc); err != nil {
			return err
		}
	}
	return nil
}

func (m *StateMigrator) applyStorageChanges(mptStorageTrie *trie.StateTrie, storageRoot common.Hash, changes map[common.Hash][]byte) (common.Hash, *trienode.NodeSet, error) {
	for hk, val := range changes {
		preimage, err := m.readZkPreimage(hk)
		if err != nil {
			return common.Hash{}, nil, err
		}
		slotKey := common.BytesToHash(preimage).Bytes()
		if (common.BytesToHash(val) == common.Hash{}) {
			if err := mptStorageTrie.DeleteStorage(common.Address{}, slotKey); err != nil {
				return common.Hash{}, nil, err
			}
		} else {
			trimmed := common.TrimLeftZeroes(common.BytesToHash(val).Bytes())
			if err := mptStorageTrie.UpdateStorage(common.Address{}, slotKey, trimmed); err != nil {
				return common.Hash{}, nil, err
			}
		}
	}

	return m.commit(mptStorageTrie, storageRoot)
}

func (m *StateMigrator) applyNewStateTransition(headNumber uint64) error {
	start := m.migratedRef.BlockNumber() + 1
	prevRoot := m.migratedRef.Root()
	for i := start; i <= headNumber; i++ {
		log.Info("Apply new state to MPT", "block", i, "prevRoot", prevRoot.TerminalString())

		tr, err := trie.NewStateTrie(trie.StateTrieID(prevRoot), m.mptdb)
		if err != nil {
			return err
		}

		destructChanges, accountChanges, storageChanges, err := core.ReadStateChanges(m.db, i)
		if err != nil {
			return err
		}
		if err := m.applyDestructChanges(tr, prevRoot, destructChanges); err != nil {
			return err
		}
		if err := m.applyAccountChanges(tr, i, prevRoot, accountChanges, storageChanges); err != nil {
			return err
		}

		root, set, err := m.commit(tr, prevRoot)
		if err != nil {
			return err
		}
		// if set is nil, it means there are no changes, so we skip verification in that case.
		if set != nil {
			if err := m.validateState(tr, set, prevRoot, i); err != nil {
				return err
			}
		}

		if err := m.migratedRef.Update(root, i); err != nil {
			return err
		}

		if err := core.DeleteStateChanges(m.db, i); err != nil {
			return err
		}

		prevRoot = root
	}

	return nil
}
