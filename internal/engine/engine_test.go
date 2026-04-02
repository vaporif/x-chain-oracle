package engine_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestMatchRuleGtCondition(t *testing.T) {
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
	eng := engine.New(rules)
	event := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			Chain:     types.ChainEthereum,
			EventType: types.EventBridgeDeposit,
			Token:     "USDC",
			Amount:    "60000000000",
			Timestamp: time.Now().Unix(),
		},
		AmountUSD: mo.Some(60000.0),
	}

	signals := eng.Evaluate(event)
	require.Len(t, signals, 1)
	assert.Equal(t, "liquidity_needed", signals[0].SignalType)
	assert.Equal(t, 0.8, signals[0].Confidence)
}

func TestMatchRuleNoMatch(t *testing.T) {
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
	eng := engine.New(rules)
	event := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType: types.EventBridgeDeposit,
			Timestamp: time.Now().Unix(),
		},
		AmountUSD: mo.Some(1000.0),
	}

	signals := eng.Evaluate(event)
	assert.Empty(t, signals)
}

func TestMatchRuleWrongTrigger(t *testing.T) {
	rules := &engine.RulesConfig{
		Rules: []engine.Rule{
			{
				Name:    "large_bridge",
				Trigger: "bridge_deposit",
				Signal:  "liquidity_needed",
			},
		},
	}
	eng := engine.New(rules)
	event := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType: types.EventDEXSwap,
			Timestamp: time.Now().Unix(),
		},
	}

	signals := eng.Evaluate(event)
	assert.Empty(t, signals)
}

func TestConditionOperators(t *testing.T) {
	tests := []struct {
		name   string
		op     string
		field  string
		value  string
		event  types.EnrichedEvent
		expect bool
	}{
		{
			name: "eq match",
			op:   "eq", field: "destination_chain", value: "solana",
			event: types.EnrichedEvent{ChainEvent: types.ChainEvent{
				EventType: types.EventBridgeDeposit,
				DestChain: mo.Some(types.ChainID("solana")),
			}},
			expect: true,
		},
		{
			name: "eq no match",
			op:   "eq", field: "destination_chain", value: "ethereum",
			event: types.EnrichedEvent{ChainEvent: types.ChainEvent{
				EventType: types.EventBridgeDeposit,
				DestChain: mo.Some(types.ChainID("solana")),
			}},
			expect: false,
		},
		{
			name: "lt match",
			op:   "lt", field: "amount_usd", value: "100",
			event: types.EnrichedEvent{
				ChainEvent: types.ChainEvent{EventType: types.EventBridgeDeposit},
				AmountUSD:  mo.Some(50.0),
			},
			expect: true,
		},
		{
			name: "gte match",
			op:   "gte", field: "amount_usd", value: "100",
			event: types.EnrichedEvent{
				ChainEvent: types.ChainEvent{EventType: types.EventBridgeDeposit},
				AmountUSD:  mo.Some(100.0),
			},
			expect: true,
		},
		{
			name: "in match",
			op:   "in", field: "token", value: "USDC,USDT,DAI",
			event: types.EnrichedEvent{ChainEvent: types.ChainEvent{
				EventType: types.EventBridgeDeposit,
				Token:     "USDT",
			}},
			expect: true,
		},
		{
			name: "in no match",
			op:   "in", field: "token", value: "USDC,USDT,DAI",
			event: types.EnrichedEvent{ChainEvent: types.ChainEvent{
				EventType: types.EventBridgeDeposit,
				Token:     "WBTC",
			}},
			expect: false,
		},
		{
			name: "contains match",
			op:   "contains", field: "source_address", value: "0xDead",
			event: types.EnrichedEvent{ChainEvent: types.ChainEvent{
				EventType:     types.EventBridgeDeposit,
				SourceAddress: "0xDeadBeef",
			}},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := &engine.RulesConfig{
				Rules: []engine.Rule{
					{
						Name:       "test_rule",
						Trigger:    string(tt.event.EventType),
						Conditions: []engine.Condition{{Field: tt.field, Op: tt.op, Value: tt.value}},
						Signal:     "test_signal",
						Confidence: 0.9,
					},
				},
			}
			eng := engine.New(rules)
			signals := eng.Evaluate(tt.event)
			if tt.expect {
				assert.Len(t, signals, 1, "expected signal for %s", tt.name)
			} else {
				assert.Empty(t, signals, "expected no signal for %s", tt.name)
			}
		})
	}
}

func TestCorrelationApprovalThenBridge(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:       "approval_then_bridge",
			Sequence:   []string{"token_approval", "bridge_deposit"},
			Window:     "30s",
			SameFields: []string{"source_address", "token"},
			Signal:     "high_confidence_bridge",
			Confidence: 0.95,
		},
	})

	now := time.Now().Unix()

	approval := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventTokenApproval,
			Token:         "USDC",
			SourceAddress: "0xSender",
			Timestamp:     now,
		},
	}
	signals := corr.Process(approval)
	assert.Empty(t, signals, "approval alone should not trigger")

	bridge := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventBridgeDeposit,
			Token:         "USDC",
			SourceAddress: "0xSender",
			Timestamp:     now + 10,
			DestChain:     mo.Some(types.ChainID("solana")),
		},
	}
	signals = corr.Process(bridge)
	require.Len(t, signals, 1)
	assert.Equal(t, "high_confidence_bridge", signals[0].SignalType)
	assert.Equal(t, 0.95, signals[0].Confidence)
}

func TestCorrelationWindowExpiry(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:       "approval_then_bridge",
			Sequence:   []string{"token_approval", "bridge_deposit"},
			Window:     "1ms",
			SameFields: []string{"source_address"},
			Signal:     "high_confidence_bridge",
			Confidence: 0.95,
		},
	})

	now := time.Now().Unix()

	corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventTokenApproval,
			SourceAddress: "0xSender",
			Timestamp:     now - 10,
		},
	})

	time.Sleep(5 * time.Millisecond)

	signals := corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventBridgeDeposit,
			SourceAddress: "0xSender",
			Timestamp:     now,
		},
	})
	assert.Empty(t, signals, "should not fire — first event expired")
}

func TestCorrelationFieldMismatch(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:       "approval_then_bridge",
			Sequence:   []string{"token_approval", "bridge_deposit"},
			Window:     "30s",
			SameFields: []string{"source_address", "token"},
			Signal:     "high_confidence_bridge",
			Confidence: 0.95,
		},
	})

	now := time.Now().Unix()

	corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventTokenApproval,
			Token:         "USDC",
			SourceAddress: "0xSender",
			Timestamp:     now,
		},
	})

	signals := corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventBridgeDeposit,
			Token:         "USDT",
			SourceAddress: "0xSender",
			Timestamp:     now + 5,
		},
	})
	assert.Empty(t, signals, "should not fire — token mismatch")
}

func TestCorrelationBurstDetection(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:          "burst_then_bridge",
			Sequence:      []string{"token_account_create", "bridge_deposit"},
			Window:        "60s",
			SameFields:    []string{"token"},
			Signal:        "burst_bridge_incoming",
			Confidence:    0.7,
			MinFirstCount: 3,
		},
	})

	now := time.Now().Unix()
	mint := "So11111111111111111111111111111111111111112"

	for i := 0; i < 3; i++ {
		signals := corr.Process(types.EnrichedEvent{
			ChainEvent: types.ChainEvent{
				EventType: types.EventTokenAccCreate,
				Token:     mint,
				Timestamp: now + int64(i),
			},
		})
		assert.Empty(t, signals)
	}

	signals := corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType: types.EventBridgeDeposit,
			Token:     mint,
			Timestamp: now + 10,
		},
	})
	require.Len(t, signals, 1)
	assert.Equal(t, "burst_bridge_incoming", signals[0].SignalType)
}

func TestCorrelationBurstNotEnough(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:          "burst_then_bridge",
			Sequence:      []string{"token_account_create", "bridge_deposit"},
			Window:        "60s",
			SameFields:    []string{"token"},
			Signal:        "burst_bridge_incoming",
			Confidence:    0.7,
			MinFirstCount: 3,
		},
	})

	now := time.Now().Unix()

	for i := 0; i < 2; i++ {
		corr.Process(types.EnrichedEvent{
			ChainEvent: types.ChainEvent{
				EventType: types.EventTokenAccCreate,
				Token:     "MINT",
				Timestamp: now + int64(i),
			},
		})
	}

	signals := corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType: types.EventBridgeDeposit,
			Token:     "MINT",
			Timestamp: now + 5,
		},
	})
	assert.Empty(t, signals, "should not fire — only 2 of required 3 first events")
}

func TestLoadRules(t *testing.T) {
	toml := `
[[rules]]
name = "large_bridge"
trigger = "bridge_deposit"
signal = "liquidity_needed"
confidence = 0.8
metadata_fields = ["destination_chain", "token"]

[[rules.conditions]]
field = "amount_usd"
op = "gt"
value = "50000"

[[correlations]]
name = "approval_then_bridge"
sequence = ["token_approval", "bridge_deposit"]
window = "30s"
same_fields = ["source_address", "token"]
signal = "high_confidence_bridge"
confidence = 0.95
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	cfg, err := engine.LoadRules(path)
	require.NoError(t, err)
	require.Len(t, cfg.Rules, 1)
	assert.Equal(t, "large_bridge", cfg.Rules[0].Name)
	assert.Len(t, cfg.Rules[0].Conditions, 1)

	require.Len(t, cfg.Correlations, 1)
	assert.Equal(t, "approval_then_bridge", cfg.Correlations[0].Name)
	assert.Equal(t, []string{"token_approval", "bridge_deposit"}, cfg.Correlations[0].Sequence)
}

func TestLoadRulesRejectsLongSequence(t *testing.T) {
	toml := `
[[correlations]]
name = "too_long"
sequence = ["a", "b", "c"]
window = "30s"
same_fields = []
signal = "bad"
confidence = 0.5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	_, err := engine.LoadRules(path)
	assert.Error(t, err, "should reject sequence with length != 2")
}
