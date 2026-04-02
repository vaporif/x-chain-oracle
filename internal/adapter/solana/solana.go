package solana

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Adapter struct {
	cfg    config.ChainConfig
	events chan types.RawEvent
}

func New(cfg config.ChainConfig) *Adapter {
	// Solana produces higher event volume than EVM chains
	return &Adapter{
		cfg:    cfg,
		events: make(chan types.RawEvent, 1024),
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	defer close(a.events)

	return adapter.WithReconnect(ctx, adapter.ReconnectConfig{
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     15 * time.Second,
		MaxRetries:     20,
	}, func(ctx context.Context) error {
		zap.L().Named("solana").Info("connecting", zap.String("url", a.cfg.RPCURL))
		// TODO: use gagliardetto/solana-go to subscribe to program logs
		<-ctx.Done()
		return ctx.Err()
	})
}

func (a *Adapter) Events() <-chan types.RawEvent { return a.events }
func (a *Adapter) Chain() types.ChainID          { return types.ChainSolana }
