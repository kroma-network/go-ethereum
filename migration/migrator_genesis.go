package migration

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/zk"
)

func zkPreimageWithAlloc(db ethdb.Database) (map[common.Hash][]byte, error) {
	genesis, err := core.ReadGenesis(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load genesis from database: %w", err)
	}
	preimages := make(map[common.Hash][]byte)
	for addr, account := range genesis.Alloc {
		hash := common.BytesToHash(zk.MustNewSecureHash(addr.Bytes()).Bytes())
		preimages[hash] = addr.Bytes()

		if account.Storage != nil {
			for key := range account.Storage {
				hash = common.BytesToHash(zk.MustNewSecureHash(key.Bytes()).Bytes())
				preimages[hash] = key.Bytes()
			}
		}
	}

	return preimages, nil
}
