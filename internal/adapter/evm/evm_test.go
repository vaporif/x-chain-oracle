package evm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestNewAdapterFields(t *testing.T) {
	cfg := config.ChainConfig{RPCURL: "wss://test.com", Mode: "websocket"}
	a := evm.New(types.ChainEthereum, cfg, nil, nil, nil, nil)

	assert.Equal(t, types.ChainEthereum, a.Chain())
	assert.NotNil(t, a.Events())
}
