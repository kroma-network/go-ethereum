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
	sanityCheckByte32Key(key)
	return z.Get(key)
}

func (z *ZkMerkleStateTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
	blob, err := z.Get(address.Bytes())
	if blob == nil || err != nil {
		return nil, err
	}
	return types.UnmarshalStateAccount(blob)
}

func (z *ZkMerkleStateTrie) UpdateStorage(_ common.Address, key, value []byte) error {
	sanityCheckByte32Key(key)
	return z.MerkleTree.UpdateUnsafe(key, 1, []zkt.Byte32{*zkt.NewByte32FromBytes(value)})
}

func (z *ZkMerkleStateTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
	sanityCheckByte32Key(address.Bytes())
	value, flag := account.MarshalFields()
	return z.MerkleTree.UpdateUnsafe(address.Bytes(), flag, value)
}

func (z *ZkMerkleStateTrie) UpdateContractCode(_ common.Address, _ common.Hash, _ []byte) error {
	return nil
}

func (z *ZkMerkleStateTrie) DeleteStorage(_ common.Address, key []byte) error {
	sanityCheckByte32Key(key)
	return z.MerkleTree.Delete(key)
}

func (z *ZkMerkleStateTrie) DeleteAccount(address common.Address) error {
	return z.MerkleTree.Delete(address.Bytes())
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
	rawdb.WriteLegacyTrieNode(z.db.diskdb, common.BytesToHash(z.RootNode().Hash().Bytes()), z.RootNode().Value())
	// Since NodeSet relies directly on mpt, we can't create a NodeSet.
	// Of course, we might be able to force-fit it by implementing the node interface.
	// However, NodeSet has been improved in geth, it could be improved to return a NodeSet when a commit is applied.
	// So let's not do that for the time being.
	// related geth commit : https://github.com/ethereum/go-ethereum/commit/bbcb5ea37b31c48a59f9aa5fad3fd22233c8a2ae
	return common.BytesToHash(z.MerkleTree.Hash()), nil, nil
}

func (z *ZkMerkleStateTrie) NodeIterator(startKey []byte) (NodeIterator, error) {
	nodeBlobFromTree, nodeBlobToIteratorNode := zkMerkleTreeNodeBlobFunctions(z.db.Get)
	return newMerkleTreeIterator(z.Hash(), nodeBlobFromTree, nodeBlobToIteratorNode, startKey), nil
}

func (z *ZkMerkleStateTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
	err := z.MerkleTree.Prove(key, func(node zk.TreeNode) error {
		if leaf, ok := node.(*zk.LeafNode); ok {
			preImage := z.GetKey(leaf.KeyHash.Bytes())
			if len(preImage) > 0 {
				leaf.KeyPreimage = &zkt.Byte32{}
				copy(leaf.KeyPreimage[:], preImage)
			}
		}
		return proofDb.Put(node.Hash()[:], node.Value())
	})
	if err != nil {
		return err
	}
	// we put this special kv pair in db so we can distinguish the type and make suitable Proof
	return proofDb.Put(magicHash, zktrie.ProofMagicBytes())
}

func (z *ZkMerkleStateTrie) GetNode(path []byte) ([]byte, int, error) { panic("implement me") }

func (z *ZkMerkleStateTrie) Update(key, value []byte) error {
	return z.MerkleTree.Update(key, value)
}

func (z *ZkMerkleStateTrie) Copy() *ZkMerkleStateTrie {
	return &ZkMerkleStateTrie{z.MerkleTree.Copy(), z.db}
}
