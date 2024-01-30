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
	return newZkMerkleStateTrie(tree, db), nil
}

func NewEmptyZkMerkleStateTrie(db *Database) *ZkMerkleStateTrie {
	return newZkMerkleStateTrie(zk.NewEmptyMerkleTree(), db)
}

func newZkMerkleStateTrie(tree *zk.MerkleTree, db *Database) *ZkMerkleStateTrie {
	trie := NewZkMerkleTrie(tree, db)
	trie.logger = log.New("trie", "ZkMerkleStateTrie")
	trie.transformKey = func(key []byte) ([]byte, error) {
		sanityCheckByte32Key(key)
		hash, err := zk.NewSecureHash(key)
		if err != nil {
			return nil, err
		}
		return hash[:], nil
	}
	return &ZkMerkleStateTrie{ZkMerkleTrie: trie, preimage: db.preimages}
}

func (z *ZkMerkleStateTrie) GetKey(kHashBytes []byte) []byte {
	// TODO: use a kv cache in memory
	k, err := zkt.NewBigIntFromHashBytes(kHashBytes)
	if err != nil {
		z.logger.Error("failed to GetKey", "error", err)
		return nil
	}
	if z.db.preimages == nil {
		return nil
	}
	return z.db.preimages.preimage(common.BytesToHash(k.Bytes()))
}

func (z *ZkMerkleStateTrie) GetStorage(_ common.Address, key []byte) ([]byte, error) {
	return z.Get(key)
}

func (z *ZkMerkleStateTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
	if blob, err := z.Get(address[:]); blob == nil || err != nil {
		return nil, err
	} else {
		return types.UnmarshalStateAccount(blob)
	}
}

func (z *ZkMerkleStateTrie) UpdateStorage(_ common.Address, key, value []byte) error {
	return z.Update(key, value)
}

func (z *ZkMerkleStateTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
	values, _ := account.MarshalFields()
	var v []byte
	for _, value := range values {
		v = append(v, value.Bytes()...)
	}
	return z.Update(address[:], v)
}

func (z *ZkMerkleStateTrie) UpdateContractCode(_ common.Address, _ common.Hash, _ []byte) error {
	return nil
}

func (z *ZkMerkleStateTrie) DeleteStorage(_ common.Address, key []byte) error { return z.Delete(key) }
func (z *ZkMerkleStateTrie) DeleteAccount(address common.Address) error       { return z.Delete(address[:]) }

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
	return newZkMerkleStateTrie(z.MerkleTree.Copy(), z.db)
}
