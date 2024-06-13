package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

var (
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
	backend        ethBackend
	db             ethdb.Database
	zkdb           *trie.Database
	mptdb          *trie.Database
	genesisAccount map[common.Hash]common.Address
	genesisStorage map[common.Hash][]byte
	tracersAPI     *tracers.API
	traceCfg       *tracers.TraceConfig
	migratedRef    *core.MigratedRef

	stopCh chan struct{}
}

func NewStateMigrator(backend ethBackend, tracersAPI *tracers.API) (*StateMigrator, error) {
	db := backend.ChainDb()

	migratedRef := core.NewMigratedRef(db)
	tracer := "prestateTracer"

	head := backend.BlockChain().CurrentBlock()
	if backend.BlockChain().Config().IsKromaMPT(head.Time) {
		return nil, errors.New("state has been already transitioned")
	}

	stopCh := make(chan struct{})
	return &StateMigrator{
		backend: backend,
		db:      db,
		zkdb: trie.NewDatabase(db, &trie.Config{
			Preimages:   true,
			Zktrie:      true,
			KromaZKTrie: true,
		}),
		mptdb:      trie.NewDatabase(db, &trie.Config{Preimages: true}),
		tracersAPI: tracersAPI,
		traceCfg: &tracers.TraceConfig{
			Tracer:       &tracer,
			TracerConfig: json.RawMessage(`{"diffMode": true}`),
		},
		migratedRef: migratedRef,
		stopCh:      stopCh,
	}, nil
}

func (m *StateMigrator) Start() {
	genesisAccount, genesisStorage, err := readGenesisAlloc(m.db)
	if err != nil {
		log.Crit("Failed to start state migrator", "error", err)
		return
	}
	m.genesisAccount = genesisAccount
	m.genesisStorage = genesisStorage

	log.Info("Starting state migrator to migrate ZKT to MPT")
	go func() {
		header := rawdb.ReadHeadHeader(m.db)
		if m.migratedRef.Number == 0 {
			log.Info("Start migrate past states")
			// Start migration from the head block. It takes long time.
			err := m.migrateAccount(header)
			if err != nil {
				log.Crit("Failed to migrate state", "error", err)
			}
			m.migratedRef.Save()

			err = m.ValidateMigratedState(m.migratedRef.Root, header.Root)
			if err != nil {
				log.Crit("Migrated state is invalid", "error", err)
			}
			log.Info("Migrated status has been validated")
		}

		log.Info("Subscribe new head event to apply new state")
		headCh := make(chan core.ChainHeadEvent, 1)
		headSub := m.backend.BlockChain().SubscribeChainHeadEvent(headCh)
		for {
			select {
			case ev := <-headCh:
				head := ev.Block
				if m.migratedRef.Number > head.NumberU64() {
					continue
				}
				err := m.applyNewStateTransition(m.migratedRef, head.NumberU64())
				if err != nil {
					log.Crit("Failed to apply new state transition", "error", err)
					return
				}
				break
			case <-m.stopCh:
				headSub.Unsubscribe()
				close(headCh)
				return
			}
		}
	}()
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

	mpt, err := trie.NewStateTrie(trie.TrieID(types.EmptyRootHash), m.mptdb)
	if err != nil {
		return err
	}
	mergedNodeSet := trienode.NewMergedNodeSet()
	zkAccIt, err := openNodeIterator(m.zkdb, header.Root, true)
	if err != nil {
		return err
	}

	var cnt uint64
	for zkAccIt.Next(false) {
		if !zkAccIt.Leaf() {
			continue
		}
		address := common.BytesToAddress(m.readPreimage(zkAccIt.LeafKey(), nil))
		fmt.Println("migrate account", address.Hex())
		acc, err := types.NewStateAccount(zkAccIt.LeafBlob(), true)
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
		log.Trace("Account updated in MPT", "account ", address.Hex(), "index", common.BytesToHash(zkAccIt.LeafKey()).Hex())
		cnt++
	}
	commitStartAt := time.Now()
	newRoot, set, err := mpt.Commit(true)
	if err != nil {
		return err
	}
	if err := mergedNodeSet.Merge(set); err != nil {
		return err
	}
	if err := m.mptdb.Update(newRoot, types.EmptyRootHash, 0, mergedNodeSet, nil); err != nil {
		return err
	}
	if err := m.mptdb.Commit(newRoot, false); err != nil {
		return err
	}
	log.Info("Account migration finished", "count", cnt, "elapsed", time.Now().Sub(commitStartAt))

	m.migratedRef.Number = header.Number.Uint64()
	m.migratedRef.Root = newRoot

	return nil
}

func (m *StateMigrator) migrateStorage(
	address common.Address,
	zkStorageRoot common.Hash,
) (common.Hash, error) {
	if zkStorageRoot == types.GetEmptyRootHash(true) {
		return types.EmptyRootHash, nil
	}
	id := trie.StorageTrieID(types.EmptyRootHash, crypto.Keccak256Hash(address.Bytes()), types.EmptyRootHash)
	mpt, err := trie.NewStateTrie(id, trie.NewDatabase(m.db, &trie.Config{Preimages: true}))
	if err != nil {
		return common.Hash{}, err
	}
	mergedNodeSet := trienode.NewMergedNodeSet()
	zkStorageIt, err := openNodeIterator(m.zkdb, zkStorageRoot, true)
	if err != nil {
		return common.Hash{}, err
	}

	var cnt uint64
	for zkStorageIt.Next(false) {
		if !zkStorageIt.Leaf() {
			continue
		}
		slot := m.readPreimage(zkStorageIt.LeafKey(), &address)
		if err := mpt.UpdateStorage(common.Address{}, slot, encodeToRlp(zkStorageIt.LeafBlob())); err != nil {
			return common.Hash{}, err
		}
		log.Trace("Updated storage slot to MPT", "contract", address.Hex(), "index", common.BytesToHash(zkStorageIt.LeafKey()).Hex())
		cnt++
	}
	commitStartAt := time.Now()
	storageRoot, set, err := mpt.Commit(true)
	if err != nil {
		return common.Hash{}, err
	}
	if err := mergedNodeSet.Merge(set); err != nil {
		return common.Hash{}, err
	}
	if err := m.mptdb.Update(storageRoot, types.EmptyRootHash, 0, mergedNodeSet, nil); err != nil {
		return common.Hash{}, err
	}
	if err := m.mptdb.Commit(storageRoot, false); err != nil {
		return common.Hash{}, err
	}
	log.Debug("Storage migration finished", "account", address, "count", cnt, "elapsed", time.Now().Sub(commitStartAt))
	return storageRoot, nil
}

func (m *StateMigrator) readPreimage(key []byte, address *common.Address) []byte {
	keyHash := *trie.IteratorKeyToHash(key, true)
	if address == nil {
		fmt.Println("preimage key hash", keyHash.Hex())
	}
	if addr, ok := m.genesisAccount[keyHash]; ok {
		return addr.Bytes()
	}
	if slot, ok := m.genesisStorage[keyHash]; ok {
		return slot
	}
	if preimage := m.zkdb.Preimage(keyHash); common.BytesToHash(zk.MustNewSecureHash(preimage).Bytes()).Hex() == keyHash.Hex() {
		return preimage
	}
	log.Crit("Preimage does not exist", "hash", keyHash.Hex(), "address", address)
	return []byte{}
}

func (m *StateMigrator) FinalizeTransition(transitionBlock *types.Block) {
	// We need to update the chain config to set the correct hardforks.
	genesisHash := rawdb.ReadCanonicalHash(m.db, 0)
	cfg := rawdb.ReadChainConfig(m.db, genesisHash)
	if cfg == nil {
		log.Crit("Chain config not found")
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
}

func openNodeIterator(triedb *trie.Database, root common.Hash, isZk bool) (trie.NodeIterator, error) {
	if isZk {
		tr, err := trie.NewZkMerkleStateTrie(root, triedb)
		if err != nil {
			return nil, err
		}
		return tr.NodeIterator(nil)
	}

	tr, err := trie.NewStateTrie(trie.StateTrieID(root), triedb)
	if err != nil {
		return nil, err
	}
	return tr.NodeIterator(nil)
}

func encodeToRlp(bytes []byte) []byte {
	trimmed := common.TrimLeftZeroes(common.BytesToHash(bytes).Bytes())
	encoded, _ := rlp.EncodeToBytes(trimmed)
	return encoded
}
