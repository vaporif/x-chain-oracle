package chainlink_test

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/price/chainlink"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type mockCaller struct {
	decimals  map[string]uint8
	answers   map[string]*big.Int
	updatedAt map[string]*big.Int
	callCount int
}

func (m *mockCaller) Decimals(_ context.Context, addr string) mo.Result[uint8] {
	if d, ok := m.decimals[addr]; ok {
		return mo.Ok(d)
	}
	return mo.Err[uint8](assert.AnError)
}

func (m *mockCaller) LatestRoundData(_ context.Context, addr string) mo.Result[chainlink.RoundData] {
	m.callCount++
	a, ok1 := m.answers[addr]
	u, ok2 := m.updatedAt[addr]
	if ok1 && ok2 {
		return mo.Ok(chainlink.RoundData{Answer: a, UpdatedAt: u})
	}
	return mo.Err[chainlink.RoundData](assert.AnError)
}

func testRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	content := `
[price_feeds.ethereum.USDC]
address = "0xChainlinkUSDC"

[price_feeds.ethereum.ETH]
address = "0xChainlinkETH"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	reg, err := registry.Load(path)
	require.NoError(t, err)
	return reg
}

func TestGetPriceUSD(t *testing.T) {
	caller := &mockCaller{
		decimals:  map[string]uint8{"0xChainlinkUSDC": 8},
		answers:   map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(100_000_000)},
		updatedAt: map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(time.Now().Unix())},
	}

	cfg := config.ChainlinkConfig{CacheTTL: 30 * time.Second}
	p := chainlink.NewWithCaller(cfg, caller, testRegistry(t), types.ChainEthereum)

	result := p.GetPriceUSD(context.Background(), "USDC")
	price, err := result.Get()
	require.NoError(t, err)
	assert.InDelta(t, 1.0, price, 0.001)
}

func TestGetPriceUSDStale(t *testing.T) {
	stale := time.Now().Add(-3 * time.Hour).Unix()
	caller := &mockCaller{
		decimals:  map[string]uint8{"0xChainlinkUSDC": 8},
		answers:   map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(100_000_000)},
		updatedAt: map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(stale)},
	}

	cfg := config.ChainlinkConfig{CacheTTL: 30 * time.Second}
	p := chainlink.NewWithCaller(cfg, caller, testRegistry(t), types.ChainEthereum)

	result := p.GetPriceUSD(context.Background(), "USDC")
	_, err := result.Get()
	assert.Error(t, err, "stale price should return error")
}

func TestGetPriceUSDUnknownToken(t *testing.T) {
	caller := &mockCaller{
		decimals: map[string]uint8{}, answers: map[string]*big.Int{}, updatedAt: map[string]*big.Int{},
	}
	cfg := config.ChainlinkConfig{CacheTTL: 30 * time.Second}
	p := chainlink.NewWithCaller(cfg, caller, testRegistry(t), types.ChainEthereum)

	result := p.GetPriceUSD(context.Background(), "UNKNOWN")
	_, err := result.Get()
	assert.Error(t, err)
}

func TestGetPriceUSDCaching(t *testing.T) {
	caller := &mockCaller{
		decimals:  map[string]uint8{"0xChainlinkUSDC": 8},
		answers:   map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(100_000_000)},
		updatedAt: map[string]*big.Int{"0xChainlinkUSDC": big.NewInt(time.Now().Unix())},
	}

	cfg := config.ChainlinkConfig{CacheTTL: 1 * time.Minute}
	p := chainlink.NewWithCaller(cfg, caller, testRegistry(t), types.ChainEthereum)

	ctx := context.Background()
	p.GetPriceUSD(ctx, "USDC")
	p.GetPriceUSD(ctx, "USDC")
	p.GetPriceUSD(ctx, "USDC")

	assert.Equal(t, 1, caller.callCount, "should only call RPC once due to caching")
}
