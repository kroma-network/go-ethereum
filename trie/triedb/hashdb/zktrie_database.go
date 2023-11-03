package hashdb

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	zktrie "github.com/kroma-network/zktrie/trie"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/triestate"
)

type ZktrieDatabase struct {
	diskdb  ethdb.Database   // Persistent storage for matured trie nodes
	cleans  *fastcache.Cache // GC friendly memory cache of clean node RLPs
	prefix  []byte
	dirties map[[sha256.Size]byte]*dirty

	lock        sync.RWMutex
	dirtiesSize common.StorageSize // Storage size of the dirty node cache (exc. metadata)
}

func NewZk(diskdb ethdb.Database, config *Config) *ZktrieDatabase {
	if config == nil {
		config = Defaults
	}
	var cleans *fastcache.Cache
	if config.CleanCacheSize > 0 {
		cleans = fastcache.New(config.CleanCacheSize)
	}
	return &ZktrieDatabase{
		diskdb:  diskdb,
		cleans:  cleans,
		dirties: make(map[[sha256.Size]byte]*dirty),
	}
}

func (db *ZktrieDatabase) Scheme() string { return rawdb.HashScheme }

func (db *ZktrieDatabase) Initialized(genesisRoot common.Hash) bool {
	return rawdb.HasLegacyTrieNode(db.diskdb, genesisRoot)
}

func (db *ZktrieDatabase) Size() (common.StorageSize, common.StorageSize) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	// db.dirtiesSize only contains the useful data in the cache, but when reporting
	// the total memory consumption, the maintenance metadata is also needed to be
	// counted.
	var metadataSize = common.StorageSize(len(db.dirties))
	return 0, db.dirtiesSize + metadataSize
}

func (db *ZktrieDatabase) Update(_ common.Hash, _ common.Hash, _ uint64, _ *trienode.MergedNodeSet, _ *triestate.Set) error {
	return nil
}

func (db *ZktrieDatabase) Commit(_ common.Hash, report bool) error {
	beforeDirtyCount, beforeDirtySize := len(db.dirties), db.dirtiesSize

	start := time.Now()
	if err := db.commitAllDirties(); err != nil {
		return err
	}
	memcacheCommitTimeTimer.Update(time.Since(start))
	memcacheCommitBytesMeter.Mark(int64(beforeDirtySize - db.dirtiesSize))
	memcacheCommitNodesMeter.Mark(int64(beforeDirtyCount - len(db.dirties)))

	logger := log.Debug
	if report {
		logger = log.Info
	}
	logger(
		"Persisted trie from memory database",
		"nodes", beforeDirtyCount-len(db.dirties),
		"size", beforeDirtySize-db.dirtiesSize,
		"time", time.Since(start),
		"livenodes", len(db.dirties),
		"livesize", db.dirtiesSize,
	)
	return nil
}

func (db *ZktrieDatabase) commitAllDirties() error {
	batch := db.diskdb.NewBatch()

	db.lock.Lock()
	for hashKey, dirty := range db.dirties {
		batch.Put(dirty.key, dirty.val)
		db.removeDirtyByHashKey(hashKey)
	}
	db.lock.Unlock()

	if err := batch.Write(); err != nil {
		return err
	}

	batch.Reset()
	return nil
}

func (db *ZktrieDatabase) Close() error { return nil }

func (db *ZktrieDatabase) Cap(_ common.StorageSize) error         { return nil }
func (db *ZktrieDatabase) Reference(_ common.Hash, _ common.Hash) {}
func (db *ZktrieDatabase) Dereference(_ common.Hash)              {}
func (db *ZktrieDatabase) Node(_ common.Hash) ([]byte, error)     { return nil, nil }

// Put saves a key:value into the Storage
func (db *ZktrieDatabase) Put(k, v []byte) error {
	db.mutexPutDirty(k, v)
	return nil
}

// Get retrieves a value from a key in the Storage
func (db *ZktrieDatabase) Get(key []byte) ([]byte, error) {
	if dirty, ok := db.mutexGetDirtyByKey(key); ok {
		return dirty.val, nil
	}
	key = db.computeKey(key)
	if db.cleans != nil {
		if enc := db.cleans.Get(nil, key); enc != nil {
			memcacheCleanHitMeter.Mark(1)
			memcacheCleanReadMeter.Mark(int64(len(enc)))
			return enc, nil
		}
	}
	v, err := db.diskdb.Get(key)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, zktrie.ErrKeyNotFound
	}
	if db.cleans != nil {
		db.cleans.Set(key[:], v)
		memcacheCleanMissMeter.Mark(1)
		memcacheCleanWriteMeter.Mark(int64(len(v)))
	}
	return v, err
}

func (db *ZktrieDatabase) mutexGetDirtyByKey(key []byte) (*dirty, bool) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	dirty, ok := db.dirties[sha256.Sum256(db.computeKey(key[:]))]
	return dirty, ok
}

func (db *ZktrieDatabase) mutexPutDirty(key, val []byte) {
	db.lock.Lock()
	defer db.lock.Unlock()

	dirty := newDirty(key, val)
	db.dirties[sha256.Sum256(db.computeKey(key[:]))] = dirty
	db.dirtiesSize += common.StorageSize(common.HashLength + dirty.size())
}

func (db *ZktrieDatabase) removeDirtyByHashKey(key [32]byte) {
	dirty := db.dirties[key]
	delete(db.dirties, key)
	db.dirtiesSize -= common.StorageSize(common.HashLength + dirty.size())
}

func (db *ZktrieDatabase) computeKey(vs ...[]byte) []byte {
	var b bytes.Buffer
	b.Write(db.prefix)
	for _, v := range vs {
		b.Write(v)
	}
	return b.Bytes()
}

// Removed the bottom two functions because they are not used anywhere
// Iterate implements the method Iterate of the interface Storage
// List implements the method List of the interface Storage

type dirty struct {
	key []byte
	val []byte
}

func newDirty(k []byte, v []byte) *dirty { return &dirty{key: k, val: v} }

func (d dirty) size() int { return len(d.key) + len(d.val) }
