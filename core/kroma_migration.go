package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethdb"
)

var HaltOnStateTransition = errors.New("historical rpc must be set to transition to MPT")

type MigratedRef struct {
	db ethdb.Database
	mu sync.Mutex

	root   common.Hash
	number uint64
}

func NewMigratedRef(db ethdb.Database) *MigratedRef {
	ref := &MigratedRef{db: db}
	if b, _ := db.Get([]byte("migration-root")); len(b) > 0 {
		ref.root = common.BytesToHash(b)
	}
	if b, _ := db.Get([]byte("migration-number")); len(b) > 0 {
		ref.number = hexutil.MustDecodeUint64(string(b))
	}
	return ref
}

func (mr *MigratedRef) Update(root common.Hash, number uint64) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	mr.root = root
	mr.number = number

	if err := mr.db.Put([]byte("migration-root"), root.Bytes()); err != nil {
		return fmt.Errorf("failed to update migrated state root: %w", err)
	}
	if err := mr.db.Put([]byte("migration-number"), []byte(hexutil.EncodeUint64(number))); err != nil {
		return fmt.Errorf("failed to update migrated block number: %w", err)
	}
	return nil
}

func (mr *MigratedRef) Root() common.Hash {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.root
}

func (mr *MigratedRef) BlockNumber() uint64 {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.number
}
