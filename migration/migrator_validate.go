package migration

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/status-im/keycard-go/hexutils"
	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type Slot struct {
	key   []byte
	value []byte
}

type StateAccount struct {
	addr  common.Address
	value *types.StateAccount
}

func (m *StateMigrator) ValidateMigratedState(mptRoot common.Hash, zkRoot common.Hash) error {
	var accounts atomic.Uint64
	var slots atomic.Uint64

	eg, cCtx := errgroup.WithContext(m.ctx)
	eg.Go(func() error {
		mpt, err := trie.NewStateTrie(trie.StateTrieID(mptRoot), m.mptdb)
		if err != nil {
			log.Error("Failed to create state trie", "root", mptRoot, "err", err)
			return err
		}

		zkt, err := trie.NewZkMerkleStateTrie(zkRoot, m.zktdb)
		if err != nil {
			log.Error("Failed to create zk state trie", "root", zkRoot, "err", err)
			return err
		}

		var mu sync.Mutex
		err = hashRangeIterator(cCtx, zkt, NumProcessAccount, func(key, value []byte) error {
			zktAcc, err := types.NewStateAccount(value, true)
			if err != nil {
				log.Error("Invalid account encountered during traversal", "err", err)
				return err
			}

			hk := trie.IteratorKeyToHash(key, true)
			preimage, err := m.readZkPreimage(*hk)
			if err != nil {
				return err
			}
			addr := common.BytesToAddress(preimage)
			mu.Lock()
			mptAcc, err := mpt.GetAccount(addr)
			mu.Unlock()
			if err != nil || mptAcc == nil {
				log.Error("Failed to get account in MPT", "address", addr, "err", err)
				return err
			}
			if mptAcc.Balance.Cmp(zktAcc.Balance) != 0 {
				return fmt.Errorf("account %s balance mismatch. expected %s, got %s", addr, zktAcc.Balance, mptAcc.Balance)
			}
			if mptAcc.Nonce != zktAcc.Nonce {
				return fmt.Errorf("account %s nonce mismatch. expected %d, got %d", addr, zktAcc.Nonce, mptAcc.Nonce)
			}
			if !bytes.Equal(mptAcc.CodeHash, zktAcc.CodeHash) {
				return fmt.Errorf("account %s codehash mismatch. expected %s, got %s", addr, common.BytesToHash(zktAcc.CodeHash), common.BytesToHash(mptAcc.CodeHash))
			}

			if zktAcc.Root != (common.Hash{}) {
				id := trie.StorageTrieID(mptRoot, crypto.Keccak256Hash(addr.Bytes()), mptAcc.Root)
				mptStorage, err := trie.NewStateTrie(id, m.mptdb)
				if err != nil {
					log.Error("Failed to create state trie", "root", mptAcc.Root, "err", err)
					return err
				}

				zktStorage, err := trie.NewZkMerkleStateTrie(zktAcc.Root, m.zktdb)
				if err != nil {
					log.Error("Failed to create zk state trie", "root", zktAcc.Root, "err", err)
					return err
				}
				var mu sync.Mutex
				err = hashRangeIterator(cCtx, zktStorage, NumProcessStorage, func(key, value []byte) error {
					hk := trie.IteratorKeyToHash(key, true)
					preimage, err := m.readZkPreimage(*hk)
					if err != nil {
						return err
					}
					slot := common.BytesToHash(preimage).Bytes()
					zktVal := common.TrimLeftZeroes(common.BytesToHash(value).Bytes())
					mu.Lock()
					mptVal, err := mptStorage.GetStorage(addr, slot)
					mu.Unlock()
					if err != nil {
						log.Error("Failed to get storage value in MPT", "err", err)
						return err
					}
					if !bytes.Equal(mptVal, zktVal) {
						return fmt.Errorf("account %s storage (slot: %s) mismatch. expected %s, got %s", addr, common.BytesToHash(slot), common.BytesToHash(zktVal), common.BytesToHash(mptVal))
					}

					slots.Add(1)
					return nil
				})
				if err != nil {
					return err
				}
			} else if mptAcc.Root != types.EmptyRootHash {
				return fmt.Errorf("account %s root should be empty root hash. got %s", addr, mptAcc.Root)
			}

			accounts.Add(1)
			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})

	// In progress logger
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	go func() {
		for ; ; <-ticker.C {
			select {
			case <-m.ctx.Done():
				return
			default:
				log.Info("Migrated state validation in progress", "accounts", accounts.Load(), "slots", slots.Load())
			}
		}
	}()

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

// if validation succeeds, it returns nil
func (m *StateMigrator) validateState(tr *trie.StateTrie, set *trienode.NodeSet, prevRoot common.Hash, bn uint64) error {
	if set == nil {
		return nil
	}
	if bn == 0 {
		return fmt.Errorf("block number must be greater than zero")
	}
	parentZktBlock := m.backend.BlockChain().GetBlockByNumber(bn - 1)
	if parentZktBlock == nil {
		return fmt.Errorf("failed to get zkBlock: %d", bn-1)
	}
	parentZkTrie, err := trie.NewZkMerkleStateTrie(parentZktBlock.Root(), m.zktdb)
	if err != nil {
		return err
	}
	zktBlock := m.backend.BlockChain().GetBlockByNumber(bn)
	if zktBlock == nil {
		return fmt.Errorf("failed to get zkBlock: %d", bn)
	}
	zkTr, err := trie.NewZkMerkleStateTrie(zktBlock.Root(), m.zktdb)
	if err != nil {
		return err
	}
	originTrie, err := trie.NewStateTrie(trie.StateTrieID(prevRoot), m.mptdb)
	if err != nil {
		return err
	}
	updatedRootNode := set.Nodes[""].Blob
	originRootNode, _, err := originTrie.GetNode([]byte(""))
	if err != nil {
		return err
	}

	var deletedLeaves []*StateAccount
	var updatedLeaves []*StateAccount
	for path, node := range set.Nodes {
		if node.IsDeleted() {
			blob, _, err := originTrie.GetNode(trie.HexToCompact(path))
			if err != nil {
				return err
			}
			if blob != nil && trie.IsLeafNode(blob) {
				hk, err := trie.GetKeyFromPath(originRootNode, m.db, []byte(path))
				if err != nil {
					return err
				}
				preimage := tr.GetKey(hk)
				if preimage == nil {
					return fmt.Errorf("failed to get preimage for hashKey: %x", hk)
				}
				addr := common.BytesToAddress(preimage)
				deletedLeaves = append(deletedLeaves, &StateAccount{addr, nil})
			}
		} else {
			if trie.IsLeafNode(node.Blob) {
				hk, err := trie.GetKeyFromPath(updatedRootNode, m.db, []byte(path))
				if err != nil {
					return err
				}
				preimage := tr.GetKey(hk)
				if preimage == nil {
					return fmt.Errorf("failed to get preimage for hashKey: %x", hk)
				}
				addr := common.BytesToAddress(preimage)
				acc, err := trie.NodeBlobToAccount(node.Blob)
				if err != nil {
					return err
				}
				// StorageRoot is already validated in storage verification stage
				if zkAcc, err := zkTr.GetAccount(addr); err != nil {
					return err
				} else if zkAcc == nil {
					acc.Root = common.Hash{}
				} else {
					acc.Root = zkAcc.Root
				}
				updatedLeaves = append(updatedLeaves, &StateAccount{addr, acc})
			}
		}
	}

	for _, leave := range deletedLeaves {
		err = parentZkTrie.DeleteAccount(leave.addr)
		if err != nil {
			return err
		}
	}
	for _, leave := range updatedLeaves {
		err = parentZkTrie.UpdateAccount(leave.addr, leave.value)
		if err != nil {
			return err
		}
	}

	zktRoot, _, err := parentZkTrie.Commit(false)
	if err != nil {
		return err
	}
	if zktRoot.Cmp(zktBlock.Root()) != 0 {
		return fmt.Errorf("invalid migrated state")
	}
	return nil
}

func (m *StateMigrator) validateStorage(tr *trie.StateTrie, id *trie.ID, addr common.Address, set *trienode.NodeSet, bn uint64) error {
	if set == nil {
		return nil
	}
	if bn == 0 {
		return fmt.Errorf("block number must be greater than zero")
	}
	parentZkBlock := m.backend.BlockChain().GetBlockByNumber(bn - 1)
	if parentZkBlock == nil {
		return fmt.Errorf("failed to get zkBlock: %d", bn-1)
	}
	parentZkTrie, err := trie.NewZkMerkleStateTrie(parentZkBlock.Root(), m.zktdb)
	if err != nil {
		return err
	}
	var parentZktRoot common.Hash
	if zkAcc, err := parentZkTrie.GetAccount(addr); err != nil {
		return err
	} else if zkAcc == nil {
		parentZktRoot = common.Hash{}
	} else {
		parentZktRoot = zkAcc.Root
	}
	parentZkt, err := trie.NewZkMerkleStateTrie(parentZktRoot, m.zktdb)
	if err != nil {
		return err
	}
	originTrie, err := trie.NewStateTrie(id, m.mptdb)
	if err != nil {
		return err
	}
	updatedRootNode := set.Nodes[""].Blob
	originRootNode, _, err := originTrie.GetNode([]byte(""))
	if err != nil {
		return err
	}

	var deletedLeaves []*Slot
	var updatedLeaves []*Slot
	for path, node := range set.Nodes {
		if node.IsDeleted() {
			blob, _, err := originTrie.GetNode(trie.HexToCompact(path))
			if err != nil {
				return err
			}

			if blob != nil && trie.IsLeafNode(blob) {
				hk, err := trie.GetKeyFromPath(originRootNode, m.db, []byte(path))
				if err != nil {
					return err
				}
				preimage := tr.GetKey(hk)
				if preimage == nil {
					return fmt.Errorf("failed to get preimage for hashKey: %x", hk)
				}
				slot := common.BytesToHash(preimage).Bytes()
				deletedLeaves = append(deletedLeaves, &Slot{slot, nil})
				if addr.Cmp(common.HexToAddress("0x51901916b0a8A67b18299bb6fA16da4D7428f9cA")) == 0 {
					if bytes.Compare(slot, hexutils.HexToBytes("5be7a1449cff78980bfa293037675a1770f220327e9386d49ba469c7210536a4")) == 0 {
						fmt.Printf("[DeleteStorage]\n")
					}
				}

			}
		} else {
			if trie.IsLeafNode(node.Blob) {
				hk, err := trie.GetKeyFromPath(updatedRootNode, m.db, []byte(path))
				if err != nil {
					return err
				}
				preimage := tr.GetKey(hk)
				if preimage == nil {
					return fmt.Errorf("failed to get preimage for hashKey: %x", hk)
				}
				slot := common.BytesToHash(preimage).Bytes()
				val, err := trie.NodeBlobToSlot(node.Blob)
				if err != nil {
					return err
				}
				updatedLeaves = append(updatedLeaves, &Slot{slot, val})
				if addr.Cmp(common.HexToAddress("0x51901916b0a8A67b18299bb6fA16da4D7428f9cA")) == 0 {
					if bytes.Compare(slot, hexutils.HexToBytes("5be7a1449cff78980bfa293037675a1770f220327e9386d49ba469c7210536a4")) == 0 {
						fmt.Printf("[UpdateStorage] path %x slot %x\n", []byte(path), slot)
					}
				}
			}
		}
	}

	for _, leave := range deletedLeaves {
		err = parentZkt.DeleteStorage(common.Address{}, leave.key)
		if err != nil {
			return err
		}
	}
	for _, leave := range updatedLeaves {
		err = parentZkt.UpdateStorage(common.Address{}, leave.key, leave.value)
		if err != nil {
			return err
		}
	}

	zktRoot, _, err := parentZkt.Commit(false)
	if err != nil {
		return err
	}
	zktBlock := m.backend.BlockChain().GetBlockByNumber(bn)
	if zktBlock == nil {
		return fmt.Errorf("failed to get zkBlock: %d", bn)
	}
	zkTrie, err := trie.NewZkMerkleStateTrie(zktBlock.Root(), m.zktdb)
	if err != nil {
		return err
	}
	if zkAcc, err := zkTrie.GetAccount(addr); err != nil {
		return err
	} else if zkAcc == nil {
		return fmt.Errorf("account doesn't exist: %s", addr.Hex())
	} else {
		if zktRoot.Cmp(zkAcc.Root) != 0 {
			//err := m.printStoragesForDebug(zkAcc.Root, parentZkt)
			//if err != nil {
			//	panic(err)
			//}
			return fmt.Errorf("invalid migrated storage of account: %s", addr.Hex())
		}
	}
	return nil
}

// TODO(Ben) : this func should be removed before this branch is merged
func (m *StateMigrator) printStoragesForDebug(expectedStorageRoot common.Hash, actualZkt *trie.ZkMerkleStateTrie) error {
	expectedZkt, err := trie.NewZkMerkleStateTrie(expectedStorageRoot, m.zktdb)
	if err != nil {
		return err
	}
	log.Info(fmt.Sprintf("expectedStorageRoot : %x\n", expectedStorageRoot))
	log.Info(fmt.Sprintf("actualtorageRoot : %x\n", actualZkt.Hash()))

	nodeIt, err := expectedZkt.NodeIterator(nil)
	if err != nil {
		return fmt.Errorf("failed to open node iterator (root: %s): %w", expectedZkt.Hash(), err)
	}
	iter := trie.NewIterator(nodeIt)
	storageNum := 0
	actualStorages := make(map[common.Hash]bool)
	for iter.Next() {
		storageNum++
		hk := trie.IteratorKeyToHash(iter.Key, true)
		preimage, err := m.readZkPreimage(*hk)
		if err != nil {
			return err
		}
		slot := common.BytesToHash(preimage).Bytes()
		zktVal := common.BytesToHash(iter.Value).Bytes()

		actualVal, err := actualZkt.GetStorage(common.Address{}, slot)
		if err != nil {
			log.Error("Failed to get storage value in MPT", "err", err)
			return err
		}
		actualStorages[common.BytesToHash(slot)] = true
		if !bytes.Equal(actualVal, zktVal) {
			log.Warn(fmt.Sprintf("expected - slot : %x, val : %x\n", slot, zktVal))
			log.Warn(fmt.Sprintf("actual - slot : %x, val : %x\n", slot, actualVal))
		} else {
			log.Info(fmt.Sprintf("passed validation - slot : %x, val : %x\n", slot, actualVal))
		}

		if err != nil {
			return err
		}
	}
	log.Info(fmt.Sprintf("expected storageNum : %d\n", storageNum))
	if iter.Err != nil {
		return fmt.Errorf("failed to traverse state trie (root: %s): %w", actualZkt.Hash(), iter.Err)
	}

	actualStorageNum := 0
	{
		nodeIt, err := actualZkt.NodeIterator(nil)
		if err != nil {
			return fmt.Errorf("failed to open actual zkt node iterator (root: %s): %w", actualZkt.Hash(), err)
		}
		iter := trie.NewIterator(nodeIt)
		for iter.Next() {
			actualStorageNum++
			hk := trie.IteratorKeyToHash(iter.Key, true)
			preimage, err := m.readZkPreimage(*hk)
			if err != nil {
				return err
			}
			slot := common.BytesToHash(preimage).Bytes()
			zktVal := common.BytesToHash(iter.Value).Bytes()

			if _, ok := actualStorages[common.BytesToHash(slot)]; !ok {
				log.Warn("actualStorage has a slot doesn't exist in expectedStorage")
				log.Warn(fmt.Sprintf("actual - slot : %x, val : %x\n", slot, zktVal))
			}
		}
		log.Info(fmt.Sprintf("actual storageNum : %d\n", actualStorageNum))
	}

	if storageNum != actualStorageNum {
		log.Warn(fmt.Sprintf("storage num is not equal : expected %d, actual %d\n", storageNum, actualStorageNum))
	}

	return nil
}
