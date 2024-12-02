package types

import (
	"math/big"
)

func (tx *Transaction) ToKromaDepositTx() (*Transaction, error) {
	if tx.Type() != DepositTxType {
		return nil, ErrTxTypeNotSupported
	}
	var kromaDep *KromaDepositTx
	if dep, ok := tx.inner.(*DepositTx); ok {
		kromaDep = &KromaDepositTx{
			SourceHash: dep.SourceHash,
			From:       dep.From,
			To:         dep.To,
			Mint:       nil,
			Value:      new(big.Int),
			Gas:        dep.Gas,
			Data:       dep.Data,
		}
		if dep.Mint != nil {
			kromaDep.Mint = new(big.Int).Set(dep.Mint)
		}
		if dep.Value != nil {
			kromaDep.Value.Set(dep.Value)
		}
	} else if dep, ok := tx.inner.(*depositTxWithNonce); ok {
		kromaDep = &KromaDepositTx{
			SourceHash: dep.SourceHash,
			From:       dep.From,
			To:         dep.To,
			Mint:       nil,
			Value:      new(big.Int),
			Gas:        dep.Gas,
			Data:       dep.Data,
		}
		if dep.Mint != nil {
			kromaDep.Mint = new(big.Int).Set(dep.Mint)
		}
		if dep.Value != nil {
			kromaDep.Value.Set(dep.Value)
		}
	} else {
		return tx, nil
	}

	return NewTx(kromaDep), nil
}
