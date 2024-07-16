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

	slots := []string{
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000000000000000000000000000002",
		"0x0000000000000000000000000000000000000000000000000000000000000003",
		"0x0000000000000000000000000000000000000000000000000000000000000004",
		"0x0000000000000000000000000000000000000000000000000000000000000005",
		"0x0000000000000000000000000000000000000000000000000000000000000006",
		"0x0000000000000000000000000000000000000000000000000000000000000007",
		"0x0000000000000000000000000000000000000000000000000000000000000066",
		"0x0000000000000000000000000000000000000000000000000000000000000067",
		"0xb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6",
		"0xb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf7",
		"0xb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf8",
		"0x21274e0784154966da0827c4d8ff52398da1ffd72d4fd4ce3bba770ef4f51046",
		"0x5acfd26b00a93d43fa7675595844d651448f11c518e88b56112d82b524be63d1",
		"0xacc99a53bbce4565f990e4e6dc196b13bdfa596d74000f1b419d698c1357e761",
		"0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103",
	}
	for _, slot := range slots {
		hash := common.BytesToHash(zk.MustNewSecureHash(common.HexToHash(slot).Bytes()).Bytes())
		preimages[hash] = common.HexToHash(slot).Bytes()
	}
	return preimages, nil
}
