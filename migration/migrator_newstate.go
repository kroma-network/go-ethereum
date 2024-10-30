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

func (m *StateMigrator) applyDestructChanges(mptStateTrie *trie.StateTrie, root common.Hash, destructChanges map[common.Address]bool) error {
	for addr := range destructChanges {
		stateAccount, err := mptStateTrie.GetAccount(addr)
		if err != nil {
			return err
		}

		if stateAccount == nil {
			continue
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
	}
	return nil
}

func (m *StateMigrator) applyAccountChanges(mptStateTrie *trie.StateTrie, root common.Hash, accountChanges map[common.Hash][]byte, storageChanges map[common.Hash]map[common.Hash][]byte) error {
	for hashedAddr, encStateAccount := range accountChanges {
		addr := common.BytesToAddress(m.readZkPreimageWithNonIteratorKey(hashedAddr))

		if encStateAccount == nil {
			return fmt.Errorf("changes in an account is nil. we did not expect it")
		}

		stateAccount, err := mptStateTrie.GetAccount(addr)
		if err != nil {
			return err
		}

		if stateAccount == nil {
			stateAccount = types.NewEmptyStateAccount(false)
		}

		mptStorageRoot := stateAccount.Root

		subChanges, exists := storageChanges[hashedAddr]
		if exists {
			id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), stateAccount.Root)
			mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
			if err != nil {
				return err
			}

			for hashedSlotKey, encSlotValue := range subChanges {
				slotKey := m.readZkPreimageWithNonIteratorKey(hashedSlotKey)
				trimmed := common.TrimLeftZeroes(common.BytesToHash(encSlotValue).Bytes())
				if err := mptStorageTrie.UpdateStorage(addr, slotKey, trimmed); err != nil {
					return err
				}
			}

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

		destructChangesKey := core.DestructChangesKey(i)
		serDestructChanges, err := m.db.Get(destructChangesKey)
		if err != nil {
			return err
		}

		if err := batch.Delete(destructChangesKey); err != nil {
			return err
		}

		destructChanges, err := core.DeserializeStateChanges[map[common.Address]bool](serDestructChanges)
		if err != nil {
			return err
		}

		if err := m.applyDestructChanges(mptStateTrie, root, destructChanges); err != nil {
			return err
		}

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

		if err := m.applyAccountChanges(mptStateTrie, root, accountChanges, storageChanges); err != nil {
			return err
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
