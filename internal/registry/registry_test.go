package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestLoadRegistry(t *testing.T) {
	toml := `
[contracts.ethereum."0xWormhole"]
name = "Wormhole Token Bridge"
protocol = "wormhole"
median_bridge_latency = "15m"

[contracts.ethereum."0xUniswap"]
name = "Uniswap V3 Router"
protocol = "uniswap"

[price_feeds.ethereum.USDC]
address = "0xChainlinkUSDC"

[price_feeds.ethereum.ETH]
address = "0xChainlinkETH"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	reg, err := registry.Load(path)
	require.NoError(t, err)

	contract, ok := reg.LookupContract(types.ChainEthereum, "0xWormhole").Get()
	assert.True(t, ok)
	assert.Equal(t, "Wormhole Token Bridge", contract.Name)
	assert.Equal(t, "wormhole", contract.Protocol)
	assert.True(t, contract.MedianBridgeLatency.IsPresent())

	assert.True(t, reg.LookupContract(types.ChainEthereum, "0xUnknown").IsAbsent())

	feed, ok := reg.LookupPriceFeed(types.ChainEthereum, "USDC").Get()
	assert.True(t, ok)
	assert.Equal(t, "0xChainlinkUSDC", feed.Address)
}
