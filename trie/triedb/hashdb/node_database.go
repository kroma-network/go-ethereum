package hashdb

import "github.com/ethereum/go-ethereum/common"

type NodeDatabase interface {
	Cap(limit common.StorageSize) error
	Reference(root common.Hash, parent common.Hash)
	Dereference(root common.Hash)
}
