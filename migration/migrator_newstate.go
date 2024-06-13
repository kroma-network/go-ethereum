package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

func (m *StateMigrator) applyAccountState(address common.Address, account *types.StateAccount, nextState map[string]any, stateRoot common.Hash) (*types.StateAccount, error) {
	if account == nil {
		account = types.NewEmptyStateAccount(false)

		if code, ok := nextState["code"]; ok {
			code, ok := code.(string)
			if !ok {
				return nil, fmt.Errorf("invalid type of code")
			}
			b := common.Hex2Bytes(strings.TrimPrefix(code, "0x"))
			account.CodeHash = crypto.Keccak256Hash(b).Bytes()
			delete(nextState, "code")
		}
	}
	if balance, ok := nextState["balance"]; ok {
		balance, ok := new(big.Int).SetString(strings.TrimPrefix(balance.(string), "0x"), 16)
		if !ok {
			return nil, fmt.Errorf("invalid type of balance")
		}
		account.Balance = balance
		delete(nextState, "balance")
	}
	if nonce, ok := nextState["nonce"]; ok {
		nonce, ok := nonce.(float64)
		if !ok {
			return nil, fmt.Errorf("invalid type of nonce")
		}
		account.Nonce = uint64(nonce)
		delete(nextState, "nonce")
	}
	if storage, ok := nextState["storage"]; ok {
		trieId := trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), account.Root)
		mpt, err := trie.NewStateTrie(trieId, m.mptdb)
		if err != nil {
			return nil, err
		}
		for key, value := range storage.(map[string]any) {
			valueBytes := common.Hex2Bytes(strings.TrimPrefix(value.(string), "0x"))
			err := mpt.UpdateStorage(common.Address{}, common.HexToHash(key).Bytes(), encodeToRlp(valueBytes))
			if err != nil {
				return nil, err
			}
		}
		account.Root, err = m.commit(mpt, account.Root)
		if err != nil {
			return nil, err
		}
		delete(nextState, "storage")
	}
	if _, ok := nextState["code"]; ok {
		return nil, fmt.Errorf("cannot apply the code diff for exists account")
	}
	if len(nextState) > 0 {
		return nil, fmt.Errorf("not all state diffs have been applied")
	}
	return account, nil
}

func (m *StateMigrator) applyNewStateTransition(parent *core.MigratedRef, headNumber uint64) error {
	for i := parent.Number + 1; i <= headNumber; i++ {
		log.Info("Apply new state to MPT", "block", i, "root", parent.Root.TerminalString(), "remaining", headNumber-parent.Number)

		mpt, err := trie.NewStateTrie(trie.StateTrieID(parent.Root), m.mptdb)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		res, err := m.tracersAPI.TraceBlockByNumber(ctx, rpc.BlockNumber(i), m.traceCfg)
		if err != nil {
			cancel()
			return err
		}
		cancel()
		for _, tx := range res {
			var result map[string]map[string]map[string]any // res["post"][address]["balance"|"nonce"|...] = value
			if err := json.Unmarshal(tx.Result.(json.RawMessage), &result); err != nil {
				return err
			}
			for key, state := range result["post"] {
				address := common.HexToAddress(key)
				acc, err := mpt.GetAccount(address)
				if err != nil {
					log.Error("Failed to get account", "address", address)
				}
				acc, err = m.applyAccountState(address, acc, state, parent.Root)
				if err != nil {
					log.Error("Failed to apply new state", "address", address, "err", err)
				}
				err = mpt.UpdateAccount(address, acc)
				if err != nil {
					log.Error("Failed to retrieve block trace", "number", i, "error", err)
					return err
				}
			}
		}
		parent.Number = i
		parent.Root, err = m.commit(mpt, parent.Root)
		if err := parent.Save(); err != nil {
			return err
		}
	}

	return nil
}

func (m *StateMigrator) commit(mpt *trie.StateTrie, parentHash common.Hash) (common.Hash, error) {
	root, set, err := mpt.Commit(true)
	if err != nil {
		return common.Hash{}, err
	}

	var hashCollidedErr error
	set.ForEachWithOrder(func(path string, n *trienode.Node) {
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
