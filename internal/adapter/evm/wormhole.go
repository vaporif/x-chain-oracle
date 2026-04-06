package evm

import (
	"fmt"

	"github.com/wormhole-foundation/wormhole/sdk/vaa"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

var wormholeChainMap = map[vaa.ChainID]types.ChainID{
	vaa.ChainIDSolana:    types.ChainSolana,
	vaa.ChainIDEthereum:  types.ChainEthereum,
	vaa.ChainIDBSC:       "bsc",
	vaa.ChainIDPolygon:   "polygon",
	vaa.ChainIDAvalanche: "avalanche",
	vaa.ChainIDArbitrum:  types.ChainArbitrum,
	vaa.ChainIDOptimism:  "optimism",
	vaa.ChainIDBase:      types.ChainBase,
}

func WormholeChainID(id uint16) types.ChainID {
	if chain, ok := wormholeChainMap[vaa.ChainID(id)]; ok {
		return chain
	}
	return types.ChainID(fmt.Sprintf("wormhole_%d", id))
}
