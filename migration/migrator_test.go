package migration

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

func TestApplyNewStateTransition(t *testing.T) {
	genesis := &core.Genesis{
		Config: params.KromaTestConfig,
	}
	chainDB := rawdb.NewMemoryDatabase()
	cacheConfig := core.DefaultCacheConfigWithScheme(rawdb.HashScheme)
	cacheConfig.Preimages = true
	cacheConfig.KromaZKTrie = true
	blockchain, _ := core.NewBlockChain(chainDB, cacheConfig, genesis, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	blockchain.TrieDB().SetBackend(true)

	eth := fakeEthBackend{
		chainDB, blockchain,
	}

	m, err := NewStateMigrator(&eth)
	require.NoError(t, err)
	head := rawdb.ReadHeadHeader(m.db)
	err = m.migrateAccount(head)
	require.NoError(t, err)
	err = m.ValidateMigratedState(m.migratedRef.Root(), head.Root)
	require.NoError(t, err)

	acc := types.NewEmptyStateAccount(true)
	acc.Balance = big.NewInt(int64(rand.Uint32() + 1))
	accounts := make(map[common.Address]*types.StateAccount)
	addr1 := common.BigToAddress(common.Big1)
	addr2 := common.BigToAddress(common.Big2)
	accounts[addr1] = acc
	accounts[addr2] = acc
	storages := make(map[common.Address]map[common.Hash][]byte)
	accStorage := make(map[common.Hash][]byte)
	for i := 0; i < 30000; i++ {
		key := common.BigToHash(big.NewInt(int64(i)))
		val := big.NewInt(int64(rand.Uint32()) + 1).Bytes()
		accStorage[key] = val
	}
	storages[addr1] = accStorage

	// 1st block
	err = addBlock(m, accounts, storages)
	head = rawdb.ReadHeadHeader(m.db)
	require.NoError(t, err)
	err = m.applyNewStateTransition(head.Number.Uint64())
	require.NoError(t, err)

	// 2nd block
	acc = types.NewEmptyStateAccount(true)
	acc.Balance = big.NewInt(int64(rand.Uint32()))
	accounts = make(map[common.Address]*types.StateAccount)
	accounts[addr1] = acc
	storages = make(map[common.Address]map[common.Hash][]byte)
	accStorage = make(map[common.Hash][]byte)

	for i := 0; i < 15000; i++ {
		key := common.BigToHash(big.NewInt(int64(i)))
		val := big.NewInt(int64(0)).Bytes() // to result in DeleteStorage()
		accStorage[key] = val
	}
	storages[addr1] = accStorage

	err = addBlock(m, accounts, storages)
	require.NoError(t, err)

	head = rawdb.ReadHeadHeader(m.db)
	err = m.applyNewStateTransition(head.Number.Uint64())
	require.NoError(t, err)
}

// the storageRoot in accountState is calculated in this function. don't need to setting storageRoot
func addBlock(m *StateMigrator, accountChanges map[common.Address]*types.StateAccount, storageChanges map[common.Address]map[common.Hash][]byte) error {
	head := rawdb.ReadHeadHeader(m.db)

	zkTrie, err := trie.NewZkMerkleStateTrie(head.Root, m.zktdb)
	if err != nil {
		return err
	}

	accounts := make(map[common.Hash][]byte)
	storages := make(map[common.Hash]map[common.Hash][]byte)

	for addr, account := range accountChanges {

		var newStorageRoot common.Hash

		acc, err := zkTrie.GetAccount(addr)
		if err != nil {
			return err
		} else if acc != nil {
			account.Root = acc.Root
		} else {
			account.Root = common.Hash{}
		}

		addrHash := crypto.MustHashing(nil, addr[:], true)
		if _, ok := storageChanges[addr]; ok {
			storages[addrHash] = make(map[common.Hash][]byte)
		}
		storageZkt, err := trie.NewZkMerkleStateTrie(account.Root, m.zktdb)
		if err != nil {
			return err
		}

		for key, val := range storageChanges[addr] {
			kHash := crypto.MustHashing(nil, key.Bytes(), true)
			storages[addrHash][kHash] = val
			if (common.BytesToHash(val) == common.Hash{}) {
				err := storageZkt.DeleteStorage(common.Address{}, key.Bytes())
				if err != nil {
					return err
				}
			} else {
				err := storageZkt.UpdateStorage(common.Address{}, key.Bytes(), val)
				if err != nil {
					return err
				}
			}
		}

		if _, ok := storageChanges[addr]; ok {
			var storageSet *trienode.NodeSet
			newStorageRoot, storageSet, err = storageZkt.Commit(true)
			if err != nil {
				return err
			}
			err = m.zktdb.Update(newStorageRoot, account.Root, 0, trienode.NewWithNodeSet(storageSet), nil)
			if err != nil {
				return err
			}
			err = m.zktdb.Commit(newStorageRoot, false)
			if err != nil {
				return err
			}
			account.Root = newStorageRoot
		}

		accounts[addrHash] = types.SlimAccountZkBytes(*account)
		if err := zkTrie.UpdateAccount(addr, account); err != nil {
			return err
		}
	}

	root, set, err := zkTrie.Commit(true)
	if err != nil {
		return err
	}
	err = m.zktdb.Update(root, head.Root, 0, trienode.NewWithNodeSet(set), nil)
	if err != nil {
		return err
	}
	err = m.zktdb.Commit(root, false)
	if err != nil {
		return err
	}
	nextBlockNumber := big.NewInt(0).Add(big.NewInt(1), head.Number)
	nextBlock := types.NewBlock(&types.Header{Number: nextBlockNumber, Root: root, ParentHash: head.Hash()}, nil, nil, nil, nil)
	err = core.WriteStateChanges(m.db, nextBlockNumber.Uint64(), nil, accounts, storages)
	if err != nil {
		return err
	}
	rawdb.WriteBlock(m.db, nextBlock)
	rawdb.WriteCanonicalHash(m.db, nextBlock.Hash(), nextBlock.NumberU64())
	rawdb.WriteHeader(m.db, nextBlock.Header())
	rawdb.WriteHeadHeaderHash(m.db, nextBlock.Hash())
	return nil
}

// chainDb, err := stack.OpenDatabaseWithFreezer("chaindata", config.DatabaseCache, config.DatabaseHandles, config.DatabaseFreezer, "eth/db/chaindata/", false)
type fakeEthBackend struct {
	chainDb    ethdb.Database
	blockchain *core.BlockChain
}

func (eth *fakeEthBackend) ChainDb() ethdb.Database {
	return eth.chainDb
}

func (eth *fakeEthBackend) BlockChain() *core.BlockChain {
	return eth.blockchain
}
