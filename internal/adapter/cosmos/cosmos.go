package cosmos

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Adapter struct {
	chainID types.ChainID
	cfg     config.ChainConfig
	events  chan types.RawEvent
}

func New(chainID types.ChainID, cfg config.ChainConfig) *Adapter {
	return &Adapter{
		chainID: chainID,
		cfg:     cfg,
		events:  make(chan types.RawEvent, 128),
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	defer close(a.events)

	return adapter.WithReconnect(ctx, adapter.ReconnectConfig{
		InitialBackoff: 2 * time.Second,
		MaxBackoff:     60 * time.Second,
		MaxRetries:     5,
	}, func(ctx context.Context) error {
		zap.L().Named("cosmos").Info("connecting",
			zap.String("chain", string(a.chainID)),
			zap.String("url", a.cfg.RPCURL),
		)
		// TODO: use tendermint websocket to subscribe to tx events
		<-ctx.Done()
		return ctx.Err()
	})
}

func (a *Adapter) Events() <-chan types.RawEvent { return a.events }
func (a *Adapter) Chain() types.ChainID          { return a.chainID }
