package normalizer_test

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestNormalizeEVMBridgeDeposit(t *testing.T) {
	raw := types.RawEvent{
		Chain:     types.ChainEthereum,
		Block:     18000000,
		TxHash:    "0xabc123",
		Timestamp: 1700000000,
		EventType: types.EventBridgeDeposit,
		Data: map[string]any{
			"token":        "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			"amount":       "1000000000",
			"sender":       "0xSender",
			"contract":     "0xWormholeBridge",
			"target_chain": "solana",
		},
	}
	event, err := normalizer.Normalize(raw)
	require.NoError(t, err)
	assert.Equal(t, types.ChainEthereum, event.Chain)
	assert.Equal(t, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", event.Token)
	assert.Equal(t, "1000000000", event.Amount)
	assert.Equal(t, "0xSender", event.SourceAddress)
	assert.Equal(t, "0xWormholeBridge", event.ContractAddress)
	assert.Equal(t, mo.Some(types.ChainID("solana")), event.DestChain)
}

func TestNormalizeEVMSwap(t *testing.T) {
	raw := types.RawEvent{
		Chain:     types.ChainEthereum,
		Block:     18000001,
		TxHash:    "0xdef456",
		Timestamp: 1700000012,
		EventType: types.EventDEXSwap,
		Data: map[string]any{
			"token":  "0xToken",
			"amount": "5000000",
			"sender": "0xSwapper",
		},
	}
	event, err := normalizer.Normalize(raw)
	require.NoError(t, err)
	assert.Equal(t, types.EventDEXSwap, event.EventType)
	assert.True(t, event.DestChain.IsAbsent())
}

func TestNormalizeSolanaTokenAccountCreate(t *testing.T) {
	raw := types.RawEvent{
		Chain:     types.ChainSolana,
		Block:     200000000,
		TxHash:    "SolTx123",
		Timestamp: 1700000050,
		EventType: types.EventTokenAccCreate,
		Data: map[string]any{
			"mint":  "So11111111111111111111111111111111111111112",
			"owner": "SolOwner123",
		},
	}
	event, err := normalizer.Normalize(raw)
	require.NoError(t, err)
	assert.Equal(t, types.ChainSolana, event.Chain)
	assert.Equal(t, "So11111111111111111111111111111111111111112", event.Token)
	assert.Equal(t, "SolOwner123", event.SourceAddress)
}

func TestNormalizeIBCSendPacket(t *testing.T) {
	raw := types.RawEvent{
		Chain:     types.ChainCosmosHub,
		Block:     15000000,
		TxHash:    "CosmosTx456",
		Timestamp: 1700000100,
		EventType: types.EventIBCSendPacket,
		Data: map[string]any{
			"token":        "uatom",
			"amount":       "10000000",
			"sender":       "cosmos1abc",
			"target_chain": "osmosis",
		},
	}
	event, err := normalizer.Normalize(raw)
	require.NoError(t, err)
	assert.Equal(t, types.ChainCosmosHub, event.Chain)
	assert.Equal(t, "uatom", event.Token)
	assert.Equal(t, mo.Some(types.ChainID("osmosis")), event.DestChain)
}

func TestNormalizeMissingRequiredField(t *testing.T) {
	raw := types.RawEvent{
		Chain:     types.ChainEthereum,
		Block:     18000000,
		TxHash:    "0xbad",
		Timestamp: 1700000000,
		EventType: types.EventBridgeDeposit,
		Data:      map[string]any{},
	}
	_, err := normalizer.Normalize(raw)
	assert.Error(t, err)
}
