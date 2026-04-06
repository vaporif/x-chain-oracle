package evm

import (
	"fmt"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

const (
	WormholeCoreAddress        = "0x98f3c9e6E3fAce36bAAd05FE09d375Ef1464288B"
	WormholeTokenBridgeAddress = "0x3ee18B2214AFF97000D974cf647E7C347E8fa585"
)

var wormholeChainMap = map[uint16]types.ChainID{
	1:  types.ChainSolana,
	2:  types.ChainEthereum,
	4:  "bsc",
	5:  "polygon",
	6:  "avalanche",
	23: types.ChainArbitrum,
	24: "optimism",
	30: types.ChainBase,
}

func WormholeChainID(id uint16) types.ChainID {
	if chain, ok := wormholeChainMap[id]; ok {
		return chain
	}
	return types.ChainID(fmt.Sprintf("wormhole_%d", id))
}
