package core

import (
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

var HaltOnStateTransition = errors.New("historical rpc must be set to transition to MPT")

type MigratedRef struct {
	db ethdb.Database

	Root   common.Hash `json:"root"`
	Number uint64      `json:"number"`
}

func NewMigratedRef(db ethdb.Database) *MigratedRef {
	ref := &MigratedRef{db: db}

	if b, _ := db.Get([]byte("migration-root")); len(b) > 0 {
		if err := json.Unmarshal(b, &ref); err != nil {
			log.Crit("invalid migration-root format: %w", err)
			return nil
		}
	}

	return ref
}

func (mr *MigratedRef) Save() error {
	data, err := json.Marshal(mr)
	if err != nil {
		return err
	}
	return mr.db.Put([]byte("migration-root"), data)
}
