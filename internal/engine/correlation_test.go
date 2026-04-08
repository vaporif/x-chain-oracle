package engine_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestMatchedEntriesConsumed(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:          "approval_then_bridge",
			Sequence:      []string{"token_approval", "bridge_deposit"},
			Window:        "30s",
			SameFields:    []string{"Chain", "ContractAddress"},
			MinFirstCount: 2,
			Signal:        "bridge_after_approval",
			Confidence:    0.8,
		},
	}, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    10000,
	}, nil)

	base := types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			Chain:           types.ChainEthereum,
			ContractAddress: "0xabc",
		},
	}

	// Add 3 first-sequence events
	for i := 0; i < 3; i++ {
		evt := base
		evt.EventType = "token_approval"
		evt.TxHash = fmt.Sprintf("tx-approval-%d", i)
		corr.Process(evt)
	}

	// First trigger: should produce a signal
	trigger1 := base
	trigger1.EventType = "bridge_deposit"
	trigger1.TxHash = "tx-bridge-1"
	signals1 := corr.Process(trigger1)
	assert.Len(t, signals1, 1, "first trigger should produce a signal")

	// Second trigger: matched entries consumed, should produce nothing
	trigger2 := base
	trigger2.EventType = "bridge_deposit"
	trigger2.TxHash = "tx-bridge-2"
	signals2 := corr.Process(trigger2)
	assert.Len(t, signals2, 0, "second trigger should produce no signal — entries consumed")
}

func TestWindowCapDropsOldest(t *testing.T) {
	corr := engine.NewCorrelator([]engine.Correlation{
		{
			Name:       "test",
			Sequence:   []string{"token_approval", "bridge_deposit"},
			Window:     "30s",
			SameFields: []string{"token"},
			Signal:     "test_signal",
			Confidence: 0.9,
		},
	}, engine.CorrelatorConfig{
		DefaultWindowTTL: 30 * time.Second,
		PruneInterval:    5 * time.Second,
		MaxWindowSize:    3,
	}, nil)

	for i := 0; i < 5; i++ {
		corr.Process(types.EnrichedEvent{
			ChainEvent: types.ChainEvent{
				EventType:     types.EventTokenApproval,
				Token:         "USDC",
				SourceAddress: "0xOld",
				Timestamp:     time.Now().Unix(),
			},
		})
	}

	signals := corr.Process(types.EnrichedEvent{
		ChainEvent: types.ChainEvent{
			EventType:     types.EventBridgeDeposit,
			Token:         "USDC",
			SourceAddress: "0xOld",
			Timestamp:     time.Now().Unix(),
		},
	})
	assert.Len(t, signals, 1, "should match — 3 entries still in window")
}
