package migration

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

func (m *StateMigrator) ValidateMigratedState(mptRoot common.Hash, zkRoot common.Hash) error {
	eg, _ := errgroup.WithContext(context.Background())

	appendAccountsHash := func(accountsHash common.Hash, address common.Address, acc *types.StateAccount, storagesHash common.Hash) common.Hash {
		packed := make([]byte, 0)
		packed = append(packed, address.Bytes()...)
		packed = append(packed, new(big.Int).SetUint64(acc.Nonce).Bytes()...)
		packed = append(packed, acc.Balance.Bytes()...)
		packed = append(packed, acc.CodeHash...)
		packed = append(packed, storagesHash.Bytes()...)
		hash := crypto.Keccak256Hash(packed[:])
		for i := 0; i < len(hash); i++ {
			accountsHash[i] ^= hash[i]
		}
		return accountsHash
	}

	appendStoragesHash := func(storagesHash common.Hash, slot []byte, value []byte) common.Hash {
		packed := make([]byte, 0)
		packed = append(packed, slot...)
		packed = append(packed, value...)
		hash := crypto.Keccak256Hash(packed[:])
		for i := 0; i < len(hash); i++ {
			storagesHash[i] ^= hash[i]
		}
		return storagesHash
	}

	mptAccChecksum := common.Hash{}
	zkAccChecksum := common.Hash{}

	mptAccIt, err := openNodeIterator(m.mptdb, mptRoot, false)
	if err != nil {
		return err
	}
	zkAccIt, err := openNodeIterator(m.zkdb, zkRoot, true)
	if err != nil {
		return err
	}

	eg.Go(func() error {
		accountChecksum := common.Hash{}

		for zkAccIt.Next(false) {
			if !zkAccIt.Leaf() {
				continue
			}
			address := common.BytesToAddress(m.readPreimage(zkAccIt.LeafKey(), nil))
			acc, err := types.NewStateAccount(zkAccIt.LeafBlob(), true)
			if err != nil {
				return err
			}

			// check storage trie
			zkStorageIt, err := openNodeIterator(m.zkdb, acc.Root, true)
			if err != nil {
				return err
			}

			storageChecksum := common.Hash{}
			for zkStorageIt.Next(false) {
				if !zkStorageIt.Leaf() {
					continue
				}

				slot := m.readPreimage(zkStorageIt.LeafKey(), &address)
				value := zkStorageIt.LeafBlob()
				storageChecksum = appendStoragesHash(storageChecksum, slot, value)
			}

			accountChecksum = appendAccountsHash(accountChecksum, address, acc, storageChecksum)
		}
		return nil
	})

	eg.Go(func() error {
		accountChecksum := common.Hash{}
		for mptAccIt.Next(false) {
			if !mptAccIt.Leaf() {
				continue
			}
			var acc *types.StateAccount
			if err := rlp.DecodeBytes(mptAccIt.LeafBlob(), &acc); err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return err
			}
			address := common.BytesToAddress(mptAccIt.LeafKey())

			storageChecksum := common.Hash{}
			if acc.Root == types.GetEmptyRootHash(true) {
				mptStorageIt, err := openNodeIterator(m.zkdb, acc.Root, false)
				if err != nil {
					return err
				}
				for mptStorageIt.Next(false) {
					if !mptStorageIt.Leaf() {
						continue
					}
					slot := m.readPreimage(mptStorageIt.LeafKey(), &address)
					value := mptStorageIt.LeafBlob()
					storageChecksum = appendStoragesHash(storageChecksum, slot, value)
				}
			}
			accountChecksum = appendAccountsHash(accountChecksum, address, acc, storageChecksum)
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return err
	}

	if !bytes.Equal(mptAccChecksum[:], zkAccChecksum[:]) {
		return fmt.Errorf("account checksum mismatch, mpt: %s, zk: %s", mptAccChecksum.Hex(), zkAccChecksum.Hex())
	}

	return nil
}
