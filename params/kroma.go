package params

import (
	"fmt"
)

func init() {
	NetworkNames[fmt.Sprintf("%d", KromaMainnetChainID)] = "KromaMainnet"
	NetworkNames[fmt.Sprintf("%d", KromaSepoliaChainID)] = "KromaSepolia"
	NetworkNames[fmt.Sprintf("%d", KromaDevnetChainID)] = "KromaDevnet"
}

type UpgradeConfig struct {
	CanyonTime uint64
}

var UpgradeConfigs = map[uint64]*UpgradeConfig{
	KromaMainnetChainID: {},
	KromaSepoliaChainID: {
		CanyonTime: 1707897600,
	},
	KromaDevnetChainID: {
		CanyonTime: 1707292800,
	},
}
