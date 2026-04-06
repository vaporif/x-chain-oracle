package engine_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

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
	})

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
