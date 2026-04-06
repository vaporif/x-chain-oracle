package evm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestWormholeChainIDMapping(t *testing.T) {
	tests := []struct {
		wormholeID uint16
		expected   types.ChainID
	}{
		{1, types.ChainSolana},
		{2, types.ChainEthereum},
		{4, "bsc"},
		{5, "polygon"},
		{6, "avalanche"},
		{23, types.ChainArbitrum},
		{24, "optimism"},
		{30, types.ChainBase},
	}
	for _, tt := range tests {
		got := evm.WormholeChainID(tt.wormholeID)
		assert.Equal(t, tt.expected, got, "wormhole chain %d", tt.wormholeID)
	}
}

func TestWormholeChainIDUnknown(t *testing.T) {
	got := evm.WormholeChainID(9999)
	assert.Equal(t, types.ChainID("wormhole_9999"), got)
}
