package telemetry_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type traceTestPriceProvider struct {
	prices map[string]float64
}

func (m *traceTestPriceProvider) GetPriceUSD(_ context.Context, token string) mo.Result[float64] {
	if p, ok := m.prices[token]; ok {
		return mo.Ok(p)
	}
	return mo.Err[float64](fmt.Errorf("no price"))
}

func traceTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	toml := `
[contracts.ethereum."0xBridge"]
name = "Wormhole Token Bridge"
protocol = "wormhole"
median_bridge_latency = "15m"

[price_feeds.ethereum.USDC]
address = "0xChainlinkUSDC"

[wormhole.ethereum]
core = "0xBridge"
token_bridge = "0x3ee18B2214AFF97000D974cf647E7C347E8fa585"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))
	reg, err := registry.Load(path)
	require.NoError(t, err)
	return reg
}

func TestTraceHierarchy(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)

	tel := telemetry.NewForTest(
		tp.Tracer("test"),
		telemetry.InitNoop().Metrics,
		config.TelemetryConfig{
			Enabled: true,
			Tracing: config.TracingConfig{
				Stages: config.StageToggles{
					Adapter: true, Normalizer: true, Enricher: true, Engine: true, Emitter: true,
				},
			},
		},
	)

	rawCh := make(chan pipeline.Traced[types.RawEvent], 10)
	chainCh := make(chan pipeline.Traced[types.ChainEvent], 10)
	enrichedCh := make(chan pipeline.Traced[types.EnrichedEvent], 10)
	signalCh := make(chan pipeline.Traced[types.Signal], 10)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := traceTestRegistry(t)
	pp := &traceTestPriceProvider{prices: map[string]float64{"USDC": 1.0}}

	rules := &engine.RulesConfig{
		Rules: []engine.Rule{{
			Name:       "test_rule",
			Trigger:    "bridge_deposit",
			Conditions: []engine.Condition{{Field: "amount_usd", Op: "gt", Value: "100"}},
			Signal:     "test_signal",
			Confidence: 0.9,
		}},
	}

	enr := enricher.New(reg, pp, 1, tel)
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	}, tel)

	go normalizer.Run(ctx, tel, rawCh, chainCh)
	go enr.Run(ctx, chainCh, enrichedCh)
	go eng.Run(ctx, enrichedCh, signalCh)

	_, adapterSpan := tel.Tracer.Start(ctx, "pipeline.adapter")
	tracedRaw := pipeline.Traced[types.RawEvent]{
		Value: types.RawEvent{
			Chain: types.ChainEthereum, Block: 1, TxHash: "0x1",
			Timestamp: time.Now().Unix(), EventType: types.EventBridgeDeposit,
			Data: map[string]any{
				"token": "USDC", "amount": "1000", "sender": "0xA", "contract": "0xB", "target_chain": "solana",
			},
		},
		Ctx:       oteltrace.ContextWithSpan(ctx, adapterSpan),
		StartedAt: time.Now(),
	}
	adapterSpan.End()

	rawCh <- tracedRaw
	close(rawCh)

	select {
	case sig := <-signalCh:
		assert.Equal(t, "test_signal", sig.Value.SignalType)
	case <-ctx.Done():
		t.Fatal("timed out waiting for signal")
	}

	tp.ForceFlush(ctx)

	spans := exporter.GetSpans()
	spanNames := make(map[string]bool)
	for _, s := range spans {
		spanNames[s.Name] = true
	}

	assert.True(t, spanNames["pipeline.adapter"], "missing adapter span")
	assert.True(t, spanNames["pipeline.normalizer"], "missing normalizer span")
	assert.True(t, spanNames["pipeline.enricher"], "missing enricher span")
	assert.True(t, spanNames["pipeline.engine.evaluate"], "missing engine span")

	traceID := spans[0].SpanContext.TraceID()
	for _, s := range spans {
		assert.Equal(t, traceID, s.SpanContext.TraceID(), "span %s has different trace ID", s.Name)
	}
}
