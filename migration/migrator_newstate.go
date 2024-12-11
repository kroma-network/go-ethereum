package migration

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
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

func (m *StateMigrator) applyAccountChanges(mptStateTrie *trie.StateTrie, root common.Hash, accountChanges map[common.Hash][]byte, storageChanges map[common.Hash]map[common.Hash][]byte) error {
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
		acc.Root = types.EmptyRootHash

		mptAcc, err := mptStateTrie.GetAccount(addr)
		if err != nil {
			return err
		}
		if mptAcc != nil {
			acc.Root = mptAcc.Root
		}

		if changes, ok := storageChanges[hk]; ok {
			id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), acc.Root)
			mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
			if err != nil {
				return err
			}
			acc.Root, err = m.applyStorageChanges(mptStorageTrie, acc.Root, changes)
			if err != nil {
				return err
			}
		}

		if err := mptStateTrie.UpdateAccount(addr, acc); err != nil {
			return err
		}
	}
	return nil
}

func (m *StateMigrator) applyStorageChanges(mptStorageTrie *trie.StateTrie, storageRoot common.Hash, changes map[common.Hash][]byte) (common.Hash, error) {
	for hk, val := range changes {
		preimage, err := m.readZkPreimage(hk)
		if err != nil {
			return common.Hash{}, err
		}
		slotKey := common.BytesToHash(preimage).Bytes()
		if (common.BytesToHash(val) == common.Hash{}) {
			if err := mptStorageTrie.DeleteStorage(common.Address{}, slotKey); err != nil {
				return common.Hash{}, err
			}
		} else {
			trimmed := common.TrimLeftZeroes(common.BytesToHash(val).Bytes())
			if err := mptStorageTrie.UpdateStorage(common.Address{}, slotKey, trimmed); err != nil {
				return common.Hash{}, err
			}
		}
	}

	return m.commit(mptStorageTrie, storageRoot)
}

func (m *StateMigrator) applyNewStateTransition(safeBlockNum uint64) error {
	startBlockNum := m.migratedRef.BlockNumber() + 1
	prevRoot := m.migratedRef.Root()
	for blockNum := startBlockNum; blockNum <= safeBlockNum; blockNum++ {
		log.Info("Apply new state to MPT", "block", blockNum, "prevRoot", prevRoot.TerminalString())

		mptStateTrie, err := trie.NewStateTrie(trie.StateTrieID(prevRoot), m.mptdb)
		if err != nil {
			return err
		}

		changes, err := core.ReadStateChanges(m.db, blockNum)
		if err != nil {
			return err
		}
		if err := m.applyDestructChanges(mptStateTrie, prevRoot, changes.Destruct); err != nil {
			return err
		}
		if err := m.applyAccountChanges(mptStateTrie, prevRoot, changes.Accounts, changes.Storages); err != nil {
			return err
		}

		root, err := m.commit(mptStateTrie, prevRoot)
		if err != nil {
			return err
		}
		// Validate that the state has been properly stored in the MPT.
		if err := m.ValidateNewState(blockNum, root, changes); err != nil {
			return err
		}
		if err := m.migratedRef.Update(root, blockNum); err != nil {
			return err
		}
		if err := core.DeleteStateChanges(m.db, blockNum); err != nil {
			return err
		}

		prevRoot = root
	}

	return nil
}
