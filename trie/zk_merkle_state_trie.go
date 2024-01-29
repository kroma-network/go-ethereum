package trie

import (
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleStateTrie struct {
	*ZkMerkleTrie
	preimage *preimageStore
}

func NewZkMerkleStateTrie(rootHash common.Hash, db *Database) (*ZkMerkleStateTrie, error) {
	tree, err := zk.NewMerkleTreeFromHash(zkt.NewHashFromBytes(rootHash.Bytes()), db.Get)
	if err != nil {
		return nil, err
	}
	return &ZkMerkleStateTrie{ZkMerkleTrie: NewZkMerkleTrie(tree, db), preimage: db.preimages}, nil
}

func NewEmptyZkMerkleTrie(db *Database) *ZkMerkleStateTrie {
	return &ZkMerkleStateTrie{ZkMerkleTrie: NewZkMerkleTrie(zk.NewEmptyMerkleTree(), db), preimage: db.preimages}
}

func (z *ZkMerkleStateTrie) GetKey(kHashBytes []byte) []byte {
	// TODO: use a kv cache in memory
	k, err := zkt.NewBigIntFromHashBytes(kHashBytes)
	if err != nil {
		log.Error("ZkMerkleStateTrie.GetKey", "error", err)
	}
	if z.db.preimages == nil {
		return nil
	}
	return z.db.preimages.preimage(common.BytesToHash(k.Bytes()))
}

func (z *ZkMerkleStateTrie) MustGet(key []byte) []byte {
	if data, err := z.get(key); err != nil {
		log.Error("ZkMerkleStateTrie.MustGet", "error", err)
		return nil
	} else {
		return data
	}
}

func (z *ZkMerkleStateTrie) GetStorage(_ common.Address, key []byte) ([]byte, error) {
	return z.get(key)
}

func (z *ZkMerkleStateTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
	if blob, err := z.get(address[:]); blob == nil || err != nil {
		return nil, err
	} else {
		return types.UnmarshalStateAccount(blob)
	}
}

func (z *ZkMerkleStateTrie) get(key []byte) ([]byte, error) {
	if hash, err := z.hash(key); err != nil {
		return nil, err
	} else {
		return z.MerkleTree.Get(hash[:])
	}
}

func (z *ZkMerkleStateTrie) MustUpdate(key, value []byte) {
	if err := z.update(key, value); err != nil {
		log.Error("ZkMerkleStateTrie.MustUpdate", "error", err)
	}
}

func (z *ZkMerkleStateTrie) UpdateStorage(_ common.Address, key, value []byte) error {
	return z.update(key, value)
}

func (z *ZkMerkleStateTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
	values, _ := account.MarshalFields()
	var v []byte
	for _, value := range values {
		v = append(v, value.Bytes()...)
	}
	return z.update(address[:], v)
}

func (z *ZkMerkleStateTrie) UpdateContractCode(_ common.Address, _ common.Hash, _ []byte) error {
	return nil
}

func (z *ZkMerkleStateTrie) update(key, value []byte) error {
	if hash, err := z.hash(key); err != nil {
		return err
	} else {
		return z.MerkleTree.Update(hash[:], value)
	}
}

func (z *ZkMerkleStateTrie) MustDelete(key []byte) {
	if err := z.delete(key); err != nil {
		log.Error("ZkMerkleStateTrie.MustDelete", "error", err)
	}
}

func (z *ZkMerkleStateTrie) DeleteStorage(_ common.Address, key []byte) error { return z.delete(key) }
func (z *ZkMerkleStateTrie) DeleteAccount(address common.Address) error       { return z.delete(address[:]) }
func (z *ZkMerkleStateTrie) delete(key []byte) error {
	if hash, err := z.hash(key); err != nil {
		return err
	} else {
		return z.MerkleTree.Delete(hash[:])
	}
}

func (z *ZkMerkleStateTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	return z.prove(common.ReverseBytes(key), proofDb, func(node zk.TreeNode) error {
		value := node.CanonicalValue()
		if leaf, ok := node.(*zk.LeafNode); ok {
			if preImage := z.GetKey(common.ReverseBytes(leaf.Key)); len(preImage) > 0 {
				value[len(value)-1] = byte(len(preImage))
				value = append(value, preImage[:]...)
			}
		}
		return proofDb.Put(node.Hash()[:], value)
	})
}

func (z *ZkMerkleStateTrie) Copy() *ZkMerkleStateTrie {
	return &ZkMerkleStateTrie{
		ZkMerkleTrie: NewZkMerkleTrie(z.MerkleTree.Copy(), z.db),
		preimage:     z.preimage,
	}
}

func (z *ZkMerkleStateTrie) hash(key []byte) (*zkt.Hash, error) {
	sanityCheckByte32Key(key)
	return zk.NewSecureHash(key)
}
