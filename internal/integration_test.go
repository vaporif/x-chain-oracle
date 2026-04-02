package internal_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type mockPrice struct {
	prices map[string]float64
}

func (m *mockPrice) GetPriceUSD(_ context.Context, token string) mo.Result[float64] {
	if p, ok := m.prices[token]; ok {
		return mo.Ok(p)
	}
	return mo.Err[float64](fmt.Errorf("no price"))
}

func setupTestRegistry(t *testing.T) *registry.Registry {
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

func TestFullPipelineRawEventToSignal(t *testing.T) {
	reg := setupTestRegistry(t)
	pp := &mockPrice{prices: map[string]float64{"USDC": 1.0}}

	rules := &engine.RulesConfig{
		Rules: []engine.Rule{
			{
				Name:       "large_bridge",
				Trigger:    "bridge_deposit",
				Conditions: []engine.Condition{{Field: "amount_usd", Op: "gt", Value: "50000"}},
				Signal:     "liquidity_needed",
				Confidence: 0.8,
			},
		},
	}

	rawEvents := make(chan types.RawEvent, 10)
	chainEvents := make(chan types.ChainEvent, 10)
	enrichedEvents := make(chan types.EnrichedEvent, 10)
	signals := make(chan types.Signal, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	enr := enricher.New(reg, pp, 2)
	eng := engine.New(rules)

	go normalizer.Run(ctx, rawEvents, chainEvents)
	go enr.Run(ctx, chainEvents, enrichedEvents)
	go eng.Run(ctx, enrichedEvents, signals)

	rawEvents <- types.RawEvent{
		Chain:     types.ChainEthereum,
		Block:     18000000,
		TxHash:    "0xTx999",
		Timestamp: time.Now().Unix(),
		EventType: types.EventBridgeDeposit,
		Data: map[string]any{
			"token":        "USDC",
			"amount":       "60000000000",
			"sender":       "0xSender",
			"contract":     "0xBridge",
			"target_chain": "solana",
		},
	}
	close(rawEvents)

	select {
	case sig, ok := <-signals:
		require.True(t, ok, "signals channel closed without producing a signal")
		assert.Equal(t, "liquidity_needed", sig.SignalType)
		assert.Equal(t, types.ChainEthereum, sig.SourceChain)
		assert.Equal(t, 0.8, sig.Confidence)
	case <-ctx.Done():
		t.Fatal("timed out waiting for signal")
	}
}

func TestPipelineNoMatchProducesNoSignal(t *testing.T) {
	reg := setupTestRegistry(t)
	pp := &mockPrice{prices: map[string]float64{"USDC": 1.0}}

	rules := &engine.RulesConfig{
		Rules: []engine.Rule{
			{
				Name:       "large_bridge",
				Trigger:    "bridge_deposit",
				Conditions: []engine.Condition{{Field: "amount_usd", Op: "gt", Value: "50000"}},
				Signal:     "liquidity_needed",
				Confidence: 0.8,
			},
		},
	}

	rawEvents := make(chan types.RawEvent, 10)
	chainEvents := make(chan types.ChainEvent, 10)
	enrichedEvents := make(chan types.EnrichedEvent, 10)
	signals := make(chan types.Signal, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	enr := enricher.New(reg, pp, 2)
	eng := engine.New(rules)

	go normalizer.Run(ctx, rawEvents, chainEvents)
	go enr.Run(ctx, chainEvents, enrichedEvents)
	go eng.Run(ctx, enrichedEvents, signals)

	rawEvents <- types.RawEvent{
		Chain:     types.ChainEthereum,
		Block:     18000000,
		TxHash:    "0xTx000",
		Timestamp: time.Now().Unix(),
		EventType: types.EventBridgeDeposit,
		Data: map[string]any{
			"token":        "USDC",
			"amount":       "100",
			"sender":       "0xSender",
			"contract":     "0xBridge",
			"target_chain": "solana",
		},
	}
	close(rawEvents)

	time.Sleep(500 * time.Millisecond)

	select {
	case sig, ok := <-signals:
		if ok {
			t.Fatalf("expected no signal, got: %+v", sig)
		}
		// channel closed without signal — correct
	default:
		// no signal available — correct
	}
}

func TestGracefulShutdown(t *testing.T) {
	reg := setupTestRegistry(t)
	pp := &mockPrice{prices: map[string]float64{}}
	rules := &engine.RulesConfig{}

	rawEvents := make(chan types.RawEvent, 10)
	chainEvents := make(chan types.ChainEvent, 10)
	enrichedEvents := make(chan types.EnrichedEvent, 10)
	signals := make(chan types.Signal, 10)

	ctx, cancel := context.WithCancel(context.Background())

	enr := enricher.New(reg, pp, 2)
	eng := engine.New(rules)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); normalizer.Run(ctx, rawEvents, chainEvents) }()
	go func() { defer wg.Done(); enr.Run(ctx, chainEvents, enrichedEvents) }()
	go func() { defer wg.Done(); eng.Run(ctx, enrichedEvents, signals) }()

	cancel()
	close(rawEvents)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not shut down in time")
	}
}
