package migration

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	zktrie "github.com/kroma-network/zktrie/types"
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

	for i := 0; i < 50; i++ {
		err = addBlockWithRandomChanges(m, []common.Address{
			common.HexToAddress("0x0000000000000000000000000000000000001234"),
			common.HexToAddress("0x0000000000000000000000000000000000001235"),
			common.HexToAddress("0x0000000000000000000000000000000000001236"),
		})
		require.NoError(t, err)
	}

	head := rawdb.ReadHeadHeader(m.db)
	err = m.migrateAccount(head)
	require.NoError(t, err)

	err = m.ValidateStateWithIterator(m.migratedRef.Root(), head.Root)
	require.NoError(t, err)

	accountsNum := 20
	var addresses []common.Address
	for i := 0; i < accountsNum; i++ {
		addresses = append(addresses, common.BigToAddress(big.NewInt(int64(i))))
	}

	for i := 0; i < 50; i++ {
		err = addBlockWithRandomChanges(m, addresses)
		require.NoError(t, err)
		head = rawdb.ReadHeadHeader(m.db)
		err = m.applyNewStateTransition(head.Number.Uint64())
		require.NoError(t, err)
	}

	destructChanges := make(map[common.Address]*types.StateAccount)

	for i := 0; i < accountsNum; i += 2 {
		destructChanges[addresses[i]] = nil
	}
	err = addBlock(m, destructChanges, nil, nil)
	require.NoError(t, err)

	head = rawdb.ReadHeadHeader(m.db)
	err = m.applyNewStateTransition(head.Number.Uint64())
	require.NoError(t, err)

	err = addBlockWithRandomChanges(m, []common.Address{addresses[0]})
	require.NoError(t, err)

	head = rawdb.ReadHeadHeader(m.db)
	err = m.applyNewStateTransition(head.Number.Uint64())
	require.NoError(t, err)
}

// the storageRoot in accountState is calculated in this function. don't need to set storageRoot outside
func addBlock(m *StateMigrator, destructChanges map[common.Address]*types.StateAccount, accountChanges map[common.Address]*types.StateAccount, storageChanges map[common.Address]map[common.Hash][]byte) error {
	head := rawdb.ReadHeadHeader(m.db)

	zkTrie, err := trie.NewZkMerkleStateTrie(head.Root, m.zktdb)
	if err != nil {
		return err
	}

	accounts := make(map[common.Hash][]byte)
	storages := make(map[common.Hash]map[common.Hash][]byte)

	for addr := range destructChanges {
		if _, ok := storageChanges[addr]; ok {
			return fmt.Errorf("storageChanges cannot include destructed acccount address")
		}

		acc, err := zkTrie.GetAccount(addr)
		if err != nil {
			return err
		}

		if acc == nil {
			return fmt.Errorf("an account to be deleted doesn't exist: %s", addr.Hex())
		}

		if acc.Root.Cmp(common.Hash{}) != 0 {
			storageZkt, err := trie.NewZkMerkleStateTrie(acc.Root, m.zktdb)
			if err != nil {
				return err
			}

			nodeIt, err := storageZkt.NodeIterator(nil)
			if err != nil {
				return fmt.Errorf("failed to open node iterator (root: %s): %w", storageZkt.Hash(), err)
			}
			iter := trie.NewIterator(nodeIt)
			for iter.Next() {
				hk := trie.IteratorKeyToHash(iter.Key, true)
				if err := storageZkt.MerkleTree.Delete(zktrie.ReverseByteOrder(hk.Bytes())); err != nil {
					return err
				}
			}
			if iter.Err != nil {
				return fmt.Errorf("failed to traverse state trie (root: %s): %w", storageZkt.Hash(), iter.Err)
			}

			root, set, err := storageZkt.Commit(false)
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
		}

		if err := zkTrie.DeleteAccount(addr); err != nil {
			return err
		}
	}

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

		if subStorageChanges, ok := storageChanges[addr]; ok {
			storages[addrHash] = make(map[common.Hash][]byte)
			storageZkt, err := trie.NewZkMerkleStateTrie(account.Root, m.zktdb)
			if err != nil {
				return err
			}

			for key, val := range subStorageChanges {
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
	err = core.WriteStateChanges(m.db, nextBlockNumber.Uint64(), destructChanges, accounts, storages)
	if err != nil {
		return err
	}
	rawdb.WriteBlock(m.db, nextBlock)
	rawdb.WriteCanonicalHash(m.db, nextBlock.Hash(), nextBlock.NumberU64())
	rawdb.WriteHeader(m.db, nextBlock.Header())
	rawdb.WriteHeadHeaderHash(m.db, nextBlock.Hash())
	return nil
}

func addBlockWithRandomChanges(m *StateMigrator, addresses []common.Address) error {
	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	accounts := make(map[common.Address]*types.StateAccount)
	storages := make(map[common.Address]map[common.Hash][]byte)

	for _, address := range addresses {
		acc := types.NewEmptyStateAccount(true)
		acc.Balance = big.NewInt(int64(r.Uint32() + 1))

		accStorage := make(map[common.Hash][]byte)
		for i := 0; i < r.Int()%100; i++ {
			key := common.BigToHash(big.NewInt(int64(i)))
			accStorage[key] = randomBigInt(r).Bytes()
		}

		accounts[address] = acc
		storages[address] = accStorage
	}

	err := addBlock(m, nil, accounts, storages)
	return err
}

func randomBigInt(r *rand.Rand) *big.Int {
	// with 20%, return value is zero
	if r.Intn(10) == 0 {
		return big.NewInt(0)
	}
	return big.NewInt(int64(r.Uint64() + uint64(1)))
}

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
