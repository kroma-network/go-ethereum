package migration

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

// BedrockTransitionBlockExtraData represents the extradata
// set in the very first bedrock block. This value must be
// less than 32 bytes long or it will create an invalid block.
var BedrockTransitionBlockExtraData = []byte("BEDROCK")

type ethBackend interface {
	ChainDb() ethdb.Database
	BlockChain() *core.BlockChain
}

type StateMigrator struct {
	backend       ethBackend
	db            ethdb.Database
	zktdb         *trie.Database
	mptdb         *trie.Database
	allocPreimage map[common.Hash][]byte
	migratedRef   *core.MigratedRef

	ctx    context.Context
	cancel context.CancelFunc
}

func NewStateMigrator(backend ethBackend) (*StateMigrator, error) {
	db := backend.ChainDb()

	allocPreimage, err := zkPreimageFromAlloc(db)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis alloc: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &StateMigrator{
		backend: backend,
		db:      db,
		zktdb: trie.NewDatabase(db, &trie.Config{
			Preimages:   true,
			Zktrie:      true,
			KromaZKTrie: backend.BlockChain().TrieDB().IsKromaZK(),
		}),
		mptdb:         trie.NewDatabase(db, &trie.Config{Preimages: true}),
		allocPreimage: allocPreimage,
		migratedRef:   core.NewMigratedRef(db),

		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (m *StateMigrator) Start() {
	log.Info("Start state migrator to migrate ZKT to MPT")
	go func() {
		if m.migratedRef.Root() == (common.Hash{}) {
			targetBlock := m.backend.BlockChain().CurrentBlock()
			if targetBlock == nil {
				targetBlock = m.backend.BlockChain().Genesis().Header()
			}

			// Wait until migration becomes possible. For migration to be possible, the target block must become safe,
			// and the state changes of the block following the target block must be stored in the db.
			log.Info("Waiting until migration becomes possible", "block", targetBlock.Number)
			m.waitForMigrationReady(targetBlock)

			// Start migration for all state up to the safe block using the zk trie iterator.
			// This process takes a long time.
			log.Info("Start migrate past state", "block", targetBlock.Number)
			err := m.migrateAccount(targetBlock)
			if err != nil {
				log.Error("Failed to migrate past state", "error", err)
				return
			}

			err = m.ValidateStateWithIterator(m.migratedRef.Root(), targetBlock.Root)
			if err != nil {
				log.Error("Migrated past state is invalid", "error", err)
				return
			}
			log.Info("Migrated past state have been validated")
		}

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		log.Info("Start a loop to apply state of new block")
		for {
			select {
			case <-ticker.C:
				safeBlock := m.backend.BlockChain().CurrentSafeBlock()
				// Skip block that have already been migrated.
				if safeBlock == nil || m.migratedRef.BlockNumber() >= safeBlock.Number.Uint64() {
					continue
				}
				if m.backend.BlockChain().Config().IsKromaMPT(safeBlock.Time) {
					return
				}
				err := m.applyNewStateTransition(safeBlock.Number.Uint64())
				if err != nil {
					log.Error("Failed to apply new state transition", "error", err)
				}
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

func (m *StateMigrator) Stop() {
	log.Info("Stopping state migrator")
	m.cancel()
}

func (m *StateMigrator) migrateAccount(header *types.Header) error {
	log.Info("Migrate account", "root", header.Root, "number", header.Number)
	startAt := time.Now()
	var accounts atomic.Uint64

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	go func() {
		for ; ; <-ticker.C {
			select {
			case <-m.ctx.Done():
				return
			default:
				log.Info("Migrate accounts in progress", "accounts", accounts.Load())
			}
		}
	}()

	mpt, err := trie.NewStateTrie(trie.TrieID(types.EmptyRootHash), m.mptdb)
	if err != nil {
		return err
	}

	zkt, err := trie.NewZkMerkleStateTrie(header.Root, m.zktdb)
	if err != nil {
		return err
	}

	nodeIt, err := zkt.NodeIterator(nil)
	if err != nil {
		return fmt.Errorf("failed to open node iterator (root: %s): %w", zkt.Hash(), err)
	}
	iter := trie.NewIterator(nodeIt)
	for iter.Next() {
		hk := trie.IteratorKeyToHash(iter.Key, true)
		preimage, err := m.readZkPreimage(*hk)
		if err != nil {
			return err
		}
		address := common.BytesToAddress(preimage)
		log.Debug("Start migrate account", "address", address.Hex())
		acc, err := types.NewStateAccount(iter.Value, true)
		if err != nil {
			return err
		}
		acc.Root, err = m.migrateStorage(address, acc.Root)
		if err != nil {
			return err
		}
		if err := mpt.UpdateAccount(address, acc); err != nil {
			return err
		}
		accounts.Add(1)
		log.Trace("Account updated in MPT", "account", address.Hex(), "index", common.BytesToHash(iter.Key).Hex())
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
		}
	}
	if iter.Err != nil {
		return fmt.Errorf("failed to traverse state trie (root: %s): %w", zkt.Hash(), iter.Err)
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

	zkt, err := trie.NewZkMerkleStateTrie(zkStorageRoot, m.zktdb)
	if err != nil {
		return common.Hash{}, err
	}

	nodeIt, err := zkt.NodeIterator(nil)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to open node iterator (root: %s): %w", zkt.Hash(), err)
	}
	iter := trie.NewIterator(nodeIt)
	var slots atomic.Uint64
	for iter.Next() {
		hk := trie.IteratorKeyToHash(iter.Key, true)
		preimage, err := m.readZkPreimage(*hk)
		if err != nil {
			return common.Hash{}, err
		}
		slot := common.BytesToHash(preimage).Bytes()
		trimmed := common.TrimLeftZeroes(common.BytesToHash(iter.Value).Bytes())
		if err := mpt.UpdateStorage(address, slot, trimmed); err != nil {
			return common.Hash{}, err
		}

		slots.Add(1)
		log.Trace("Updated storage slot to MPT", "contract", address.Hex(), "index", common.BytesToHash(iter.Key).Hex())
	}
	if iter.Err != nil {
		return common.Hash{}, fmt.Errorf("failed to traverse state trie (root: %s): %w", zkt.Hash(), iter.Err)
	}

	root, err := m.commit(mpt, types.EmptyRootHash)
	if err != nil {
		return common.Hash{}, err
	}
	log.Debug("Storage migration finished", "account", address, "slots", slots.Load(), "elapsed", time.Since(startAt))
	return root, nil
}

func (m *StateMigrator) readZkPreimage(hashKey common.Hash) ([]byte, error) {
	if preimage, ok := m.allocPreimage[hashKey]; ok {
		return preimage, nil
	}
	if preimage := m.zktdb.Preimage(hashKey); preimage != nil {
		if common.BytesToHash(zk.MustNewSecureHash(preimage).Bytes()).Hex() == hashKey.Hex() {
			return preimage, nil
		}
	}
	return []byte{}, fmt.Errorf("preimage does not exist: %s", hashKey.Hex())
}

func (m *StateMigrator) commit(mpt *trie.StateTrie, parentHash common.Hash) (common.Hash, error) {
	root, set, err := mpt.Commit(true)
	if err != nil {
		return common.Hash{}, err
	}
	if set == nil {
		log.Warn("Tried to commit state changes, but nothing has changed", "root", root)
		return root, nil
	}

	// NOTE(pangssu): It is possible that the keccak256 and poseidon hashes collide, and data loss can occur.
	for path, mptNode := range set.Nodes {
		if mptNode.IsDeleted() {
			continue
		}
		data, _ := m.db.Get(mptNode.Hash.Bytes())
		if len(data) == 0 {
			continue
		}
		if node, err := zk.NewTreeNodeFromBlob(data); err == nil {
			return common.Hash{}, fmt.Errorf("hash collision detected: hashKey: %v, path: %v, data: %v, zkNode: %v", mptNode.Hash, path, data, node.Hash())
		}
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
	cfg := m.backend.BlockChain().Config()
	// Set the BedrockBlock to transition block number.
	cfg.BedrockBlock = transitionBlock.Number()
	// Copy KromaConfig to OptimismConfig.
	cfg.Optimism = &params.OptimismConfig{
		EIP1559Denominator:       cfg.Kroma.EIP1559Denominator,
		EIP1559Elasticity:        cfg.Kroma.EIP1559Elasticity,
		EIP1559DenominatorCanyon: cfg.Kroma.EIP1559DenominatorCanyon,
	}
	// Keep it set to true for genesis validation.
	cfg.Zktrie = true

	// Write the chain config to disk.
	genesisHash := rawdb.ReadCanonicalHash(m.db, 0)
	rawdb.WriteChainConfig(m.db, genesisHash, cfg)
	log.Info("Wrote chain config", "bedrock-block", cfg.BedrockBlock, "zktrie", cfg.Zktrie)

	// Switch trie backend to MPT
	cfg.Zktrie = false
	m.backend.BlockChain().TrieDB().SetBackend(false)
}

func (m *StateMigrator) waitForMigrationReady(target *types.Header) {
	safe := m.backend.BlockChain().CurrentSafeBlock()
	// There is no gap between the safe block and the unsafe block, no waiting is required.
	if safe != nil && safe.Number.Uint64() == target.Number.Uint64() {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Wait until the given target block becomes a safe block.
			safe = m.backend.BlockChain().CurrentSafeBlock()
			if safe == nil || safe.Number.Uint64() < target.Number.Uint64() {
				continue
			}
			// Wait until the state changes of the given target block are stored in the database.
			_, err := core.ReadStateChanges(m.db, target.Number.Uint64()+1)
			if err != nil {
				continue
			}
			return
		case <-m.ctx.Done():
			return
		}
	}
}
