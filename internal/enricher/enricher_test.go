package enricher_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type mockPriceProvider struct {
	prices map[string]float64
}

func (m *mockPriceProvider) GetPriceUSD(_ context.Context, token string) mo.Result[float64] {
	if p, ok := m.prices[token]; ok {
		return mo.Ok(p)
	}
	return mo.Err[float64](fmt.Errorf("no price for %s", token))
}

func newTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	toml := `
[contracts.ethereum."0xBridge"]
name = "Wormhole Token Bridge"
protocol = "wormhole"
median_bridge_latency = "15m"

[price_feeds.ethereum.USDC]
address = "0xChainlinkUSDC"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))
	reg, err := registry.Load(path)
	require.NoError(t, err)
	return reg
}

func TestEnrichWithKnownContract(t *testing.T) {
	reg := newTestRegistry(t)
	pp := &mockPriceProvider{prices: map[string]float64{"USDC": 1.0}}

	ce := types.ChainEvent{
		Chain:           types.ChainEthereum,
		Block:           100,
		TxHash:          "0xTx123",
		EventType:       types.EventBridgeDeposit,
		Token:           "USDC",
		Amount:          "50000000000",
		SourceAddress:   "0xSender",
		ContractAddress: "0xBridge",
	}

	result := enricher.Enrich(context.Background(), ce, reg, pp)
	assert.Equal(t, mo.Some("Wormhole Token Bridge"), result.ContractName)
	assert.Equal(t, mo.Some("wormhole"), result.Protocol)
	assert.Equal(t, mo.Some(50000000000.0), result.AmountUSD)
}

func TestEnrichWithUnknownContract(t *testing.T) {
	reg := newTestRegistry(t)
	pp := &mockPriceProvider{prices: map[string]float64{}}

	ce := types.ChainEvent{
		Chain:           types.ChainEthereum,
		TxHash:          "0xTx456",
		EventType:       types.EventDEXSwap,
		Token:           "UNKNOWN_TOKEN",
		ContractAddress: "0xUnknown",
	}

	result := enricher.Enrich(context.Background(), ce, reg, pp)
	assert.True(t, result.ContractName.IsAbsent())
	assert.True(t, result.Protocol.IsAbsent())
	assert.True(t, result.AmountUSD.IsAbsent())
}

func TestEnricherPipeline(t *testing.T) {
	reg := newTestRegistry(t)
	pp := &mockPriceProvider{prices: map[string]float64{"USDC": 1.0}}

	in := make(chan pipeline.Traced[types.ChainEvent], 2)
	out := make(chan pipeline.Traced[types.EnrichedEvent], 2)

	e := enricher.New(reg, pp, 2, telemetry.InitNoop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, in, out)

	in <- pipeline.NewTraced(context.Background(), types.ChainEvent{
		Chain:           types.ChainEthereum,
		TxHash:          "0xTx789",
		EventType:       types.EventBridgeDeposit,
		Token:           "USDC",
		Amount:          "1000000",
		ContractAddress: "0xBridge",
	})
	close(in)

	result := <-out
	assert.Equal(t, "USDC", result.Value.Token)
}
