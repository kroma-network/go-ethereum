package migration

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

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
		accountStateDiffKey := state.AccountPrefixForMigration(i)
		serializedAccounts, err := m.db.Get(accountStateDiffKey)
		batch.Delete(accountStateDiffKey)

		if err != nil {
			return err
		}
		b := new(bytes.Buffer)
		if _, err := b.Write(serializedAccounts); err != nil {
			return err
		}
		d := gob.NewDecoder(b)

		var deserializedAccounts map[common.Hash][]byte
		err = d.Decode(&deserializedAccounts)
		if err != nil {
			return err
		}

		storageStateDiffKey := state.StoragePrefixForMigration(i)
		serializedStorages, err := m.db.Get(storageStateDiffKey)
		batch.Delete(storageStateDiffKey)

		if err != nil {
			return err
		}

		b = new(bytes.Buffer)
		if _, err := b.Write(serializedStorages); err != nil {
			return err
		}
		d = gob.NewDecoder(b)

		var deserializedStorages map[common.Hash]map[common.Hash][]byte
		err = d.Decode(&deserializedStorages)
		if err != nil {
			return err
		}

		for hashedAddress, encodedAccountState := range deserializedAccounts {

			addr := common.BytesToAddress(m.readZkPreimageWithNonIteratorKey(hashedAddress))
			stateAccount, err := mptStateTrie.GetAccount(addr)

			if err != nil {
				return err
			}

			if encodedAccountState == nil {
				if err := mptStateTrie.DeleteAccount(addr); err != nil {
					return err
				}
				continue
			}

			if stateAccount == nil {
				stateAccount = types.NewEmptyStateAccount(false)
			}

			id := trie.StorageTrieID(root, crypto.Keccak256Hash(addr.Bytes()), stateAccount.Root)
			mptStorageTrie, err := trie.NewStateTrie(id, m.mptdb)
			if err != nil {
				return err
			}

			_, exists := deserializedStorages[hashedAddress]

			for hashedSlotKey, encodedSlotValue := range deserializedStorages[hashedAddress] {
				slotKey := m.readZkPreimageWithNonIteratorKey(hashedSlotKey)
				trimmed := common.TrimLeftZeroes(common.BytesToHash(encodedSlotValue).Bytes())
				if err := mptStorageTrie.UpdateStorage(addr, slotKey, trimmed); err != nil {
					return err
				}
			}

			mptStorageRoot := stateAccount.Root
			if exists {
				mptStorageRoot, err = m.commit(mptStorageTrie, stateAccount.Root)
				if err != nil {
					return err
				}
			}

			zktAccountState, err := types.NewStateAccount(encodedAccountState, true)
			if err != nil {
				return err
			}

			zktAccountState.Root = mptStorageRoot

			if err := mptStateTrie.UpdateAccount(addr, zktAccountState); err != nil {
				return err
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
