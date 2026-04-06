package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type SubscriptionStrategy interface {
	Subscribe(ctx context.Context, filter ethereum.FilterQuery) (<-chan ethtypes.Log, ethereum.Subscription, error)
}

type Adapter struct {
	chain      types.ChainID
	cfg        config.ChainConfig
	reg        *registry.Registry
	events     chan types.RawEvent
	decoder    *DecoderRegistry
	cache      *BlockCache
	strategy   SubscriptionStrategy
	httpClient *ethclient.Client
}

func New(chain types.ChainID, cfg config.ChainConfig, reg *registry.Registry, strategy SubscriptionStrategy, httpClient *ethclient.Client) *Adapter {
	return &Adapter{
		chain:      chain,
		cfg:        cfg,
		reg:        reg,
		events:     make(chan types.RawEvent, 256),
		decoder:    NewDecoderRegistry(),
		cache:      NewBlockCache(),
		strategy:   strategy,
		httpClient: httpClient,
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	defer close(a.events)
	logger := zap.L().Named("evm")

	reconnCfg := adapter.ReconnectConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		MaxRetries:     10,
	}

	return adapter.WithReconnect(ctx, reconnCfg, func(ctx context.Context) error {
		logger.Info("connecting to EVM RPC",
			zap.String("chain", string(a.chain)),
			zap.String("url", a.cfg.RPCURL),
			zap.String("mode", a.cfg.Mode),
		)

		strategy := a.strategy
		if strategy == nil {
			s, err := a.createStrategy()
			if err != nil {
				return err
			}
			strategy = s
		}

		filter := a.buildFilterQuery()

		logs, sub, err := strategy.Subscribe(ctx, filter)
		if err != nil {
			return err
		}
		defer sub.Unsubscribe()

		logger.Info("subscribed to logs",
			zap.String("chain", string(a.chain)),
			zap.Int("contract_count", len(filter.Addresses)),
		)

		for {
			select {
			case log, ok := <-logs:
				if !ok {
					return nil
				}
				a.processLog(ctx, logger, log)
			case err := <-sub.Err():
				if err != nil {
					logger.Warn("subscription error, will reconnect", zap.Error(err))
					return err
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
}

func (a *Adapter) createStrategy() (SubscriptionStrategy, error) {
	if a.cfg.Mode == "polling" {
		client := a.httpClient
		if client == nil {
			httpURL := DeriveHTTPURL(a.cfg.RPCURL)
			c, err := ethclient.Dial(httpURL)
			if err != nil {
				return nil, err
			}
			client = c
		}
		return NewPollingStrategy(client, 12*time.Second), nil
	}
	client, err := ethclient.Dial(a.cfg.RPCURL)
	if err != nil {
		return nil, err
	}
	return NewWebSocketStrategy(client), nil
}

func (a *Adapter) buildFilterQuery() ethereum.FilterQuery {
	return ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress(WormholeCoreAddress)},
		Topics:    [][]common.Hash{{LogMessagePublishedTopicHash()}},
	}
}

func (a *Adapter) processLog(ctx context.Context, logger *zap.Logger, log ethtypes.Log) {
	rawEvent, err := a.decoder.Decode(a.chain, log)
	if err != nil {
		logger.Debug("skipping undecoded log",
			zap.String("tx", log.TxHash.Hex()),
			zap.Error(err),
		)
		return
	}

	ts, ok := a.cache.Get(log.BlockNumber)
	if !ok {
		ts = a.fetchBlockTimestamp(ctx, logger, log.BlockNumber)
		if ts > 0 {
			a.cache.Set(log.BlockNumber, ts)
		}
	}
	rawEvent.Timestamp = ts

	logger.Debug("raw event received",
		zap.String("chain", string(a.chain)),
		zap.Uint64("block", log.BlockNumber),
		zap.String("tx", log.TxHash.Hex()),
		zap.String("event_type", string(rawEvent.EventType)),
	)

	select {
	case a.events <- rawEvent:
	case <-ctx.Done():
	}
}

func (a *Adapter) fetchBlockTimestamp(ctx context.Context, logger *zap.Logger, block uint64) int64 {
	client := a.httpClient
	if client == nil {
		httpURL := DeriveHTTPURL(a.cfg.RPCURL)
		c, err := ethclient.DialContext(ctx, httpURL)
		if err != nil {
			logger.Warn("failed to dial for block header", zap.Error(err))
			return 0
		}
		defer c.Close()
		client = c
	}

	header, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(block))
	if err != nil {
		logger.Warn("failed to fetch block header",
			zap.Uint64("block", block),
			zap.Error(err),
		)
		return 0
	}
	return int64(header.Time)
}

func (a *Adapter) Events() <-chan types.RawEvent {
	return a.events
}

func (a *Adapter) Chain() types.ChainID {
	return a.chain
}
