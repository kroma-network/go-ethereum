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
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"maps"
	"math/big"
	"strings"
)

type prestateStorage map[string]string

type prestateAccount struct {
	Balance string          `json:"balance,omitempty"`
	Nonce   *uint64         `json:"nonce,omitempty"`
	Code    string          `json:"code,omitempty"`
	Storage prestateStorage `json:"storage,omitempty"`
	Deleted bool            `json:"deleted,omitempty"`
}

type prestateTracerResult struct {
	Pre  map[string]prestateAccount `json:"pre"`
	Post map[string]prestateAccount `json:"post"`
}

func (m *StateMigrator) updateAccount(stateTrie *trie.StateTrie, stateRoot common.Hash, address common.Address, changes prestateAccount) error {
	account, _ := stateTrie.GetAccount(address)
	if account == nil {
		account = types.NewEmptyStateAccount(false)
		// CodeHash can only be set for empty state account.
		if len(changes.Code) != 0 {
			b := common.Hex2Bytes(strings.TrimPrefix(changes.Code, "0x"))
			account.CodeHash = crypto.Keccak256Hash(b).Bytes()
		}
	}

	if changes.Deleted {
		account = types.NewEmptyStateAccount(false)
	}

	if len(changes.Balance) != 0 {
		balance, ok := new(big.Int).SetString(strings.TrimPrefix(changes.Balance, "0x"), 16)
		if !ok {
			return fmt.Errorf("invalid type of balance")
		}
		account.Balance = balance
	}
	if changes.Nonce != nil {
		account.Nonce = *changes.Nonce
	}
	if len(changes.Storage) != 0 {
		if bytes.Equal(account.CodeHash, types.EmptyCodeHash.Bytes()) {
			return nil
		}
		trieId := trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), account.Root)
		storageTrie, err := trie.NewStateTrie(trieId, m.mptdb)
		if err != nil {
			return err
		}
		for key, value := range changes.Storage {
			slot := common.HexToHash(key).Bytes()
			if len(value) == 0 {
				if err := storageTrie.DeleteStorage(common.Address{}, slot); err != nil {
					return err
				}
			} else {
				valueBytes := common.Hex2Bytes(strings.TrimPrefix(value, "0x"))
				trimmed := common.TrimLeftZeroes(valueBytes)
				if err := storageTrie.UpdateStorage(common.Address{}, slot, trimmed); err != nil {
					return err
				}
			}
		}
		account.Root, err = m.commit(storageTrie, account.Root)
		if err != nil {
			return err
		}
	}

	return stateTrie.UpdateAccount(address, account)
}

func (m *StateMigrator) applyEip4788PostState(stateTrie *trie.StateTrie, stateRoot common.Hash, timestamp *big.Int, beaconRoot common.Hash) error {
	account, _ := stateTrie.GetAccount(params.BeaconRootsStorageAddress)
	// Skip if the account does not have a contract code.
	if account == nil || bytes.Equal(account.CodeHash, types.EmptyCodeHash.Bytes()) {
		return nil
	}
	// See https://eips.ethereum.org/EIPS/eip-4788
	bufferLength := new(big.Int).SetUint64(8191)
	timestampIdx := new(big.Int).Mod(timestamp, bufferLength)
	rootIdx := new(big.Int).Add(timestampIdx, bufferLength)

	storage := make(prestateStorage)
	storage[common.BigToHash(timestampIdx).Hex()] = common.BigToHash(timestamp).Hex()
	storage[common.BigToHash(rootIdx).Hex()] = beaconRoot.Hex()
	err := m.updateAccount(stateTrie, stateRoot, params.BeaconRootsStorageAddress, prestateAccount{Storage: storage})
	if err != nil {
		return fmt.Errorf("failed to apply eip47888 post state: %w", err)
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

func (m *StateMigrator) mergeTracerResult(res prestateTracerResult) map[string]prestateAccount {
	ret := make(map[string]prestateAccount)

	for addr, pre := range res.Pre {
		acc := prestateAccount{
			Balance: pre.Balance,
			Nonce:   pre.Nonce,
			Code:    pre.Code,
			Storage: pre.Storage,
		}
		if _, ok := res.Post[addr]; !ok {
			acc.Deleted = true
		}
		ret[addr] = acc
	}

	for addr, post := range res.Post {
		if acc, modified := ret[addr]; modified {
			if len(post.Balance) > 0 && acc.Balance != post.Balance {
				acc.Balance = post.Balance
			}
			if post.Nonce != nil && acc.Nonce != post.Nonce {
				acc.Nonce = post.Nonce
			}
			if len(post.Code) > 0 && acc.Code != post.Code {
				acc.Code = post.Code
			}
			storage := make(prestateStorage)
			maps.Copy(storage, acc.Storage)
			maps.Copy(storage, post.Storage)
			if len(storage) > 0 {
				for k := range storage {
					if _, ok := post.Storage[k]; !ok {
						storage[k] = ""
					}
				}
				acc.Storage = storage
			}
			ret[addr] = acc
		} else {
			ret[addr] = prestateAccount{
				Balance: post.Balance,
				Nonce:   post.Nonce,
				Code:    post.Code,
				Storage: post.Storage,
			}
		}
	}

	return ret
}
