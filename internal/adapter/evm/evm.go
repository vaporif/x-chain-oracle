package evm

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Adapter struct {
	chain  types.ChainID
	cfg    config.ChainConfig
	reg    *registry.Registry
	events chan types.RawEvent
}

func New(chain types.ChainID, cfg config.ChainConfig, reg *registry.Registry) *Adapter {
	return &Adapter{
		chain:  chain,
		cfg:    cfg,
		reg:    reg,
		events: make(chan types.RawEvent, 256),
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	defer close(a.events)

	reconnCfg := adapter.ReconnectConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		MaxRetries:     10,
	}

	return adapter.WithReconnect(ctx, reconnCfg, func(ctx context.Context) error {
		zap.L().Named("evm").Info("connecting to EVM RPC",
			zap.String("chain", string(a.chain)),
			zap.String("url", a.cfg.RPCURL),
			zap.String("mode", a.cfg.Mode),
		)
		<-ctx.Done()
		return ctx.Err()
	})
}

func (a *Adapter) Events() <-chan types.RawEvent {
	return a.events
}

func (a *Adapter) Chain() types.ChainID {
	return a.chain
}

func ParseLogToRawEvent(
	chain types.ChainID,
	block uint64,
	txHash string,
	timestamp int64,
	contractAddr string,
	data map[string]any,
	eventType types.EventType,
) types.RawEvent {
	data["contract"] = contractAddr
	return types.RawEvent{
		Chain:     chain,
		Block:     block,
		TxHash:    txHash,
		Timestamp: timestamp,
		EventType: eventType,
		Data:      data,
	}
}
