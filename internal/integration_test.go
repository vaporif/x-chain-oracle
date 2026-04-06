package internal_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goethereum "github.com/ethereum/go-ethereum"

	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

const (
	testWormholeCoreAddress        = "0x98f3c9e6E3fAce36bAAd05FE09d375Ef1464288B"
	testWormholeTokenBridgeAddress = "0x3ee18B2214AFF97000D974cf647E7C347E8fa585"
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
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	})

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
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	})

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

type mockStrategy struct {
	logs chan ethtypes.Log
}

func (m *mockStrategy) Subscribe(_ context.Context, _ goethereum.FilterQuery) (<-chan ethtypes.Log, goethereum.Subscription, error) {
	return m.logs, &mockSub{errCh: make(chan error)}, nil
}

type mockSub struct {
	errCh chan error
}

func (s *mockSub) Err() <-chan error { return s.errCh }
func (s *mockSub) Unsubscribe()      { close(s.errCh) }

func buildWormholeTransferPayload(amount *big.Int, tokenAddr common.Address, tokenChain, recipientChain uint16) []byte {
	payload := make([]byte, 133)
	payload[0] = 1
	amountBytes := amount.Bytes()
	copy(payload[1+32-len(amountBytes):33], amountBytes)
	copy(payload[33+12:65], tokenAddr.Bytes())
	binary.BigEndian.PutUint16(payload[65:67], tokenChain)
	binary.BigEndian.PutUint16(payload[99:101], recipientChain)
	return payload
}

func buildLogMessagePublishedData(sequence uint64, nonce uint32, payload []byte, consistencyLevel uint8) []byte {
	seqWord := make([]byte, 32)
	binary.BigEndian.PutUint64(seqWord[24:], sequence)
	nonceWord := make([]byte, 32)
	binary.BigEndian.PutUint32(nonceWord[28:], nonce)
	offsetWord := make([]byte, 32)
	binary.BigEndian.PutUint64(offsetWord[24:], 128)
	clWord := make([]byte, 32)
	clWord[31] = consistencyLevel
	lenWord := make([]byte, 32)
	binary.BigEndian.PutUint64(lenWord[24:], uint64(len(payload)))
	paddedPayload := make([]byte, ((len(payload)+31)/32)*32)
	copy(paddedPayload, payload)

	var data []byte
	data = append(data, seqWord...)
	data = append(data, nonceWord...)
	data = append(data, offsetWord...)
	data = append(data, clWord...)
	data = append(data, lenWord...)
	data = append(data, paddedPayload...)
	return data
}

func TestWormholeBridgeDepositEndToEnd(t *testing.T) {
	reg := setupTestRegistry(t)

	pp := &mockPrice{prices: map[string]float64{
		strings.ToLower("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"): 1.0,
	}}

	rules := &engine.RulesConfig{
		Rules: []engine.Rule{
			{
				Name:    "large_bridge",
				Trigger: "bridge_deposit",
				Conditions: []engine.Condition{
					{Field: "amount_usd", Op: "gt", Value: "50000"},
					{Field: "destination_chain", Op: "eq", Value: "solana"},
				},
				Signal:         "liquidity_needed",
				Confidence:     0.8,
				MetadataFields: []string{"destination_chain", "token"},
			},
		},
	}

	tokenAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	amount := new(big.Int).SetUint64(60_000_000_000)
	payload := buildWormholeTransferPayload(amount, tokenAddr, 2, 1)
	data := buildLogMessagePublishedData(1, 0, payload, 1)
	senderTopic := common.HexToHash("0x000000000000000000000000" + testWormholeTokenBridgeAddress[2:])

	wormholeLog := ethtypes.Log{
		Address:     common.HexToAddress(testWormholeCoreAddress),
		Topics:      []common.Hash{crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)")), senderTopic},
		Data:        data,
		BlockNumber: 18_000_000,
		TxHash:      common.HexToHash("0xabc123"),
	}

	logCh := make(chan ethtypes.Log, 1)
	logCh <- wormholeLog
	close(logCh)
	strategy := &mockStrategy{logs: logCh}

	cfg := config.ChainConfig{
		RPCURL:      "wss://fake",
		Mode:        "websocket",
		EventBuffer: 256,
		Backoff:     config.BackoffConfig{Initial: time.Second, Max: 30 * time.Second, MaxRetries: 10},
	}
	a := evm.New(types.ChainEthereum, cfg, reg, strategy, nil, nil)

	rawEvents := make(chan types.RawEvent, 10)
	chainEvents := make(chan types.ChainEvent, 10)
	enrichedEvents := make(chan types.EnrichedEvent, 10)
	signals := make(chan types.Signal, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var adapterWg sync.WaitGroup
	adapterWg.Add(1)
	go func() {
		defer adapterWg.Done()
		_ = a.Start(ctx)
		for event := range a.Events() {
			rawEvents <- event
		}
	}()
	go func() {
		adapterWg.Wait()
		close(rawEvents)
	}()

	enr := enricher.New(reg, pp, 2)
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	})

	go normalizer.Run(ctx, rawEvents, chainEvents)
	go enr.Run(ctx, chainEvents, enrichedEvents)
	go eng.Run(ctx, enrichedEvents, signals)

	select {
	case sig, ok := <-signals:
		require.True(t, ok)
		assert.Equal(t, "liquidity_needed", sig.SignalType)
		assert.Equal(t, types.ChainEthereum, sig.SourceChain)
		destChain, hasDest := sig.DestinationChain.Get()
		assert.True(t, hasDest)
		assert.Equal(t, types.ChainSolana, destChain)
		assert.Equal(t, 0.8, sig.Confidence)
	case <-ctx.Done():
		t.Fatal("timed out waiting for signal")
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
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	})

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
