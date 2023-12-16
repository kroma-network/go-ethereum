package trie

import (
	zktrie "github.com/kroma-network/zktrie/trie"
	zkt "github.com/kroma-network/zktrie/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/zk"
)

type ZkMerkleStateTrie struct {
	*zk.MerkleTree
	db *Database
}

func NewZkMerkleStateTrie(tree *zk.MerkleTree, db *Database) *ZkMerkleStateTrie {
	return &ZkMerkleStateTrie{MerkleTree: tree, db: db}
}

func (z *ZkMerkleStateTrie) GetKey(kHashBytes []byte) []byte {
	// TODO: use a kv cache in memory
	k, err := zkt.NewBigIntFromHashBytes(kHashBytes)
	if err != nil {
		log.Error("Unhandled trie error in ZkMerkleStateTrie.GetKey", "err", err)
	}
	if z.db.preimages == nil {
		return nil
	}
	return z.db.preimages.preimage(common.BytesToHash(k.Bytes()))
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

// Update does not hash the key.
func (z *ZkMerkleStateTrie) Update(key, value []byte) error {
	return z.MerkleTree.Update(key, value)
}

func (z *ZkMerkleStateTrie) update(key, value []byte) error {
	if hash, err := z.hash(key); err != nil {
		return err
	} else {
		return z.MerkleTree.Update(hash[:], value)
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

func (z *ZkMerkleStateTrie) Hash() common.Hash {
	hash, _, _ := z.Commit(false)
	return hash
}

func (z *ZkMerkleStateTrie) Commit(_ bool) (common.Hash, *trienode.NodeSet, error) {
	err := z.ComputeAllNodeHash(func(node zk.TreeNode) error { return z.db.Put(node.Hash()[:], node.CanonicalValue()) })
	if err != nil {
		log.Error("Failed to commit zk merkle trie", "err", err)
	}
	// There is a bug where root node is saved twice.
	// It is because of the bottom rawdb.WriteLegacyTrieNode, and we will remove it after checking if it can be removed.
	rawdb.WriteLegacyTrieNode(z.db.diskdb, common.BytesToHash(z.RootNode().Hash().Bytes()), z.RootNode().CanonicalValue())
	// Since NodeSet relies directly on mpt, we can't create a NodeSet.
	// Of course, we might be able to force-fit it by implementing the node interface.
	// However, NodeSet has been improved in geth, it could be improved to return a NodeSet when a commit is applied.
	// So let's not do that for the time being.
	// related geth commit : https://github.com/ethereum/go-ethereum/commit/bbcb5ea37b31c48a59f9aa5fad3fd22233c8a2ae
	return common.BytesToHash(z.RootNode().Hash().Bytes()), nil, nil
}

func (z *ZkMerkleStateTrie) NodeIterator(startKey []byte) (NodeIterator, error) {
	nodeBlobFromTree, nodeBlobToIteratorNode := zkMerkleTreeNodeBlobFunctions(z.db.Get)
	return newMerkleTreeIterator(z.Hash(), nodeBlobFromTree, nodeBlobToIteratorNode, startKey), nil
}

func (z *ZkMerkleStateTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	err := z.MerkleTree.Prove(key, func(node zk.TreeNode) error {
		value := node.CanonicalValue()
		if leaf, ok := node.(*zk.LeafNode); ok {
			if preImage := z.GetKey(zkt.ReverseByteOrder(leaf.Key)); len(preImage) > 0 {
				value[len(value)-1] = byte(len(preImage))
				value = append(value, preImage[:]...)
			}
		}
		return proofDb.Put(node.Hash()[:], value)
	})
	if err != nil {
		return err
	}
	// we put this special kv pair in db so we can distinguish the type and make suitable Proof
	return proofDb.Put(magicHash, zktrie.ProofMagicBytes())
}

func (z *ZkMerkleStateTrie) GetNode(path []byte) ([]byte, int, error) { panic("implement me") }

func (z *ZkMerkleStateTrie) Copy() *ZkMerkleStateTrie {
	return &ZkMerkleStateTrie{z.MerkleTree.Copy(), z.db}
}

func (z *ZkMerkleStateTrie) hash(key []byte) (*zkt.Hash, error) {
	sanityCheckByte32Key(key)
	return zk.NewSecureHash(key)
}
