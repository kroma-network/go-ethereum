package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

var (
	tracerType = "prestateTracer"
	// BedrockTransitionBlockExtraData represents the extradata
	// set in the very first bedrock block. This value must be
	// less than 32 bytes long or it will create an invalid block.
	BedrockTransitionBlockExtraData = []byte("BEDROCK")
)

type ethBackend interface {
	ChainDb() ethdb.Database
	BlockChain() *core.BlockChain
}

type StateMigrator struct {
	backend       ethBackend
	db            ethdb.Database
	zkdb          *trie.Database
	mptdb         *trie.Database
	allocPreimage map[common.Hash][]byte
	tracersAPI    *tracers.API
	traceCfg      *tracers.TraceConfig
	migratedRef   *core.MigratedRef

	stopCh chan struct{}
}

func NewStateMigrator(backend ethBackend, tracersAPI *tracers.API) *StateMigrator {
	db := backend.ChainDb()

	allocPreimage, err := zkPreimageWithAlloc(db)
	if err != nil {
		log.Crit("Failed to read genesis alloc", "err", err)
	}
	return &StateMigrator{
		backend: backend,
		db:      db,
		zkdb: trie.NewDatabase(db, &trie.Config{
			Preimages:   true,
			Zktrie:      true,
			KromaZKTrie: backend.BlockChain().TrieDB().IsKromaZK(),
		}),
		mptdb:         trie.NewDatabase(db, &trie.Config{Preimages: true}),
		allocPreimage: allocPreimage,
		tracersAPI:    tracersAPI,
		traceCfg: &tracers.TraceConfig{
			Tracer:       &tracerType,
			TracerConfig: json.RawMessage(`{"diffMode": true}`),
		},
		migratedRef: core.NewMigratedRef(db),
		stopCh:      make(chan struct{}),
	}
}

func (m *StateMigrator) Start() error {
	head := m.backend.BlockChain().CurrentSafeBlock()
	if head != nil && m.backend.BlockChain().Config().IsKromaMPT(head.Time) {
		return errors.New("state has been already transitioned")
	}

	log.Info("Start state migrator to migrate ZKT to MPT")
	go func() {
		if m.migratedRef.Root().Cmp(types.EmptyRootHash) == 0 {
			if head == nil {
				genesisHash := rawdb.ReadCanonicalHash(m.db, 0)
				head = rawdb.ReadHeader(m.db, genesisHash, 0)
			}
			log.Info("Start migrate past state")
			// Start migration from the head block. It takes long time.
			err := m.migrateAccount(head)
			if err != nil {
				log.Crit("Failed to migrate state", "error", err)
			}

			err = m.ValidateMigratedState(m.migratedRef.Root(), head.Root)
			if err != nil {
				log.Crit("Migrated state is invalid", "error", err)
			}
			log.Info("Migrated past state have been validated")
		}

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		log.Info("Start a loop to apply state of new block")
		for {
			select {
			case <-ticker.C:
				currentBlock := m.backend.BlockChain().CurrentSafeBlock()
				// Skip block that have already been migrated.
				if currentBlock == nil || m.migratedRef.BlockNumber() >= currentBlock.Number.Uint64() {
					continue
				}
				if m.backend.BlockChain().Config().IsKromaMPT(currentBlock.Time) {
					return
				}
				err := m.applyNewStateTransition(currentBlock.Number.Uint64())
				if err != nil {
					log.Error("Failed to apply new state transition", "error", err)
				}
			case <-m.stopCh:
				return
			}
		}
	}()
	return nil
}

func (m *StateMigrator) Stop() {
	log.Info("Stopping state migrator")
	close(m.stopCh)
}

func (m *StateMigrator) MigratedRef() *core.MigratedRef {
	return m.migratedRef
}

func (m *StateMigrator) migrateAccount(header *types.Header) error {
	log.Info("Migrate account", "root", header.Root, "number", header.Number)
	startAt := time.Now()
	var accounts atomic.Uint64

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				log.Info("Migrate accounts in progress", "accounts", accounts.Load())
			case <-ctx.Done():
				return
			}
		}
	}()

	mpt, err := trie.NewStateTrie(trie.TrieID(types.EmptyRootHash), m.mptdb)
	if err != nil {
		return err
	}

	zkt, err := trie.NewZkMerkleStateTrie(header.Root, m.zkdb)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	err = hashRangeIterator(zkt, NumProcessAccount, func(key, value []byte) error {
		accounts.Add(1)
		hk := trie.IteratorKeyToHash(key, true)
		address := common.BytesToAddress(m.readZkPreimage(*hk))
		log.Debug("Start migrate account", "address", address.Hex())
		acc, err := types.NewStateAccount(value, true)
		if err != nil {
			return err
		}
		acc.Root, err = m.migrateStorage(address, acc.Root)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if err := mpt.UpdateAccount(address, acc); err != nil {
			return err
		}
		log.Trace("Account updated in MPT", "account", address.Hex(), "index", common.BytesToHash(key).Hex())
		return nil
	})
	if err != nil {
		return err
	}

	root, err := m.commit(mpt, types.EmptyRootHash)
	if err != nil {
		return err
	}
	log.Info("Account migration finished", "accounts", accounts.Load(), "elapsed", time.Since(startAt))

	if err := m.migratedRef.Update(root, header.Number.Uint64()); err != nil {
		return err
	}
	return nil
}

func (m *StateMigrator) migrateStorage(
	address common.Address,
	zkStorageRoot common.Hash,
) (common.Hash, error) {
	startAt := time.Now()
	log.Debug("Start migrate storage", "address", address.Hex())
	if zkStorageRoot == (common.Hash{}) {
		return types.EmptyRootHash, nil
	}

	id := trie.StorageTrieID(types.EmptyRootHash, crypto.Keccak256Hash(address.Bytes()), types.EmptyRootHash)
	mpt, err := trie.NewStateTrie(id, m.mptdb)
	if err != nil {
		return common.Hash{}, err
	}

	zkt, err := trie.NewZkMerkleStateTrie(zkStorageRoot, m.zkdb)
	if err != nil {
		return common.Hash{}, err
	}

	var mu sync.Mutex
	var slots atomic.Uint64
	err = hashRangeIterator(zkt, NumProcessStorage, func(key, value []byte) error {
		slots.Add(1)
		hk := trie.IteratorKeyToHash(key, true)
		slot := m.readZkPreimage(*hk)
		trimmed := common.TrimLeftZeroes(common.BytesToHash(value).Bytes())
		mu.Lock()
		defer mu.Unlock()
		if err := mpt.UpdateStorage(address, slot, trimmed); err != nil {
			return err
		}
		log.Trace("Updated storage slot to MPT", "contract", address.Hex(), "index", common.BytesToHash(key).Hex())
		return nil
	})
	if err != nil {
		return common.Hash{}, err
	}

	root, err := m.commit(mpt, types.EmptyRootHash)
	if err != nil {
		return common.Hash{}, err
	}
	log.Debug("Storage migration finished", "account", address, "slots", slots.Load(), "elapsed", time.Since(startAt))
	return root, nil
}

func (m *StateMigrator) readZkPreimage(hashKey common.Hash) []byte {
	if preimage, ok := m.allocPreimage[hashKey]; ok {
		return preimage
	}
	if preimage := m.zkdb.Preimage(hashKey); preimage != nil {
		if common.BytesToHash(zk.MustNewSecureHash(preimage).Bytes()).Hex() == hashKey.Hex() {
			return preimage
		}
	}
	panic("preimage does not exist: " + hashKey.Hex())
}

func (m *StateMigrator) commit(mpt *trie.StateTrie, parentHash common.Hash) (common.Hash, error) {
	root, set, err := mpt.Commit(true)
	if err != nil {
		return common.Hash{}, err
	}
	if set == nil {
		log.Warn("Tried to commit state changes, but nothing has changed.", "root", root)
		return root, nil
	}

	var hashCollidedErr error
	set.ForEachWithOrder(func(path string, n *trienode.Node) {
		if hashCollidedErr != nil {
			return
		}
		// NOTE(pangssu): It is possible that the keccak256 and poseidon hashes collide, and data loss can occur.
		data, _ := m.db.Get(n.Hash.Bytes())
		if len(data) == 0 {
			return
		}
		if node, err := zk.NewTreeNodeFromBlob(data); err == nil {
			hashCollidedErr = fmt.Errorf("Hash collision detected: hashKey: %v, key: %v, value: %v, zkNode: %v", n.Hash.Bytes(), path, data, node)
		}
	})
	if hashCollidedErr != nil {
		return common.Hash{}, hashCollidedErr
	}

	if err := m.mptdb.Update(root, parentHash, 0, trienode.NewWithNodeSet(set), nil); err != nil {
		return common.Hash{}, err
	}
	if err := m.mptdb.Commit(root, false); err != nil {
		return common.Hash{}, err
	}
	return root, nil
}

func (m *StateMigrator) FinalizeTransition(transitionBlock types.Block) {
	// We need to update the chain config to set the correct hardforks.
	genesisHash := rawdb.ReadCanonicalHash(m.db, 0)
	cfg := rawdb.ReadChainConfig(m.db, genesisHash)
	if cfg == nil {
		panic("chain config not found")
	}

	// Set the standard options.
	cfg.LondonBlock = transitionBlock.Number()
	cfg.ArrowGlacierBlock = transitionBlock.Number()
	cfg.GrayGlacierBlock = transitionBlock.Number()
	cfg.MergeNetsplitBlock = transitionBlock.Number()
	cfg.TerminalTotalDifficulty = big.NewInt(0)
	cfg.TerminalTotalDifficultyPassed = true

	// Set the Optimism options.
	cfg.BedrockBlock = transitionBlock.Number()
	// Enable Regolith from the start of Bedrock
	cfg.RegolithTime = new(uint64)
	// Switch KromaConfig to OptimismConfig
	cfg.Optimism = &params.OptimismConfig{
		EIP1559Denominator:       cfg.Kroma.EIP1559Denominator,
		EIP1559Elasticity:        cfg.Kroma.EIP1559Elasticity,
		EIP1559DenominatorCanyon: cfg.Kroma.EIP1559DenominatorCanyon,
	}
	cfg.Zktrie = true

	// Write the chain config to disk.
	rawdb.WriteChainConfig(m.db, genesisHash, cfg)

	m.backend.BlockChain().Config().BedrockBlock = cfg.BedrockBlock
	m.backend.BlockChain().Config().RegolithTime = cfg.RegolithTime
	m.backend.BlockChain().Config().Optimism = cfg.Optimism
	m.backend.BlockChain().Config().Zktrie = false
	m.backend.BlockChain().TrieDB().SetBackend(false)

	// Yay!
	log.Info(
		"Wrote chain config",
		"1559-denominator", cfg.Optimism.EIP1559Denominator,
		"1559-elasticity", cfg.Optimism.EIP1559Elasticity,
		"1559-denominator-canyon", cfg.Optimism.EIP1559DenominatorCanyon,
	)

	// Perform a final validation of all migrated state. This takes a long time.
	go func() {
		startAt := time.Now()
		log.Info("Start validation for all migrated state")
		zkBlock := m.backend.BlockChain().GetBlockByNumber(m.migratedRef.BlockNumber())
		if zkBlock == nil {
			panic(fmt.Errorf("zk block %d not found", m.migratedRef.BlockNumber()))
		}
		if err := m.ValidateMigratedState(m.migratedRef.Root(), zkBlock.Root()); err != nil {
			panic(err)
		}
		log.Info("All migrated state have been validated", "elapsed", time.Since(startAt))
	}()
}
