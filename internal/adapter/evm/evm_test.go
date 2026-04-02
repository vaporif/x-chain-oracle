package evm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestParseTransferLog(t *testing.T) {
	raw := evm.ParseLogToRawEvent(
		types.ChainEthereum,
		18000000,
		"0xabcdef",
		1700000000,
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		map[string]any{
			"token":        "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			"amount":       "1000000000",
			"sender":       "0xSender",
			"target_chain": "solana",
		},
		types.EventBridgeDeposit,
	)

	assert.Equal(t, types.ChainEthereum, raw.Chain)
	assert.Equal(t, uint64(18000000), raw.Block)
	assert.Equal(t, "0xabcdef", raw.TxHash)
	assert.Equal(t, types.EventBridgeDeposit, raw.EventType)
	assert.Equal(t, "1000000000", raw.Data["amount"])
	assert.Equal(t, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", raw.Data["contract"])
}
