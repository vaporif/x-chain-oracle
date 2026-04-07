package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/status"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type SubscriptionStrategy interface {
	Subscribe(ctx context.Context, filter ethereum.FilterQuery) (<-chan ethtypes.Log, ethereum.Subscription, error)
}

type Adapter struct {
	chain      types.ChainID
	cfg        config.ChainConfig
	reg        *registry.Registry
	events     chan pipeline.Traced[types.RawEvent]
	decoder    *DecoderRegistry
	cache      *BlockCache
	strategy   SubscriptionStrategy
	httpClient *ethclient.Client
	tracker    *status.Tracker
	tuning     config.TuningConfig
	tel        *telemetry.Telemetry
}

func New(chain types.ChainID, cfg config.ChainConfig, reg *registry.Registry, strategy SubscriptionStrategy, httpClient *ethclient.Client, tracker *status.Tracker, tuning config.TuningConfig, tel *telemetry.Telemetry) *Adapter {
	tokenBridgeAddr := ""
	if reg != nil {
		if wh, ok := reg.WormholeConfig(chain); ok {
			tokenBridgeAddr = wh.TokenBridge
		}
	}
	return &Adapter{
		chain:      chain,
		cfg:        cfg,
		reg:        reg,
		events:     make(chan pipeline.Traced[types.RawEvent], cfg.EventBuffer),
		decoder:    NewDecoderRegistry(tokenBridgeAddr),
		cache:      NewBlockCache(tuning.BlockCacheSize),
		strategy:   strategy,
		httpClient: httpClient,
		tracker:    tracker,
		tuning:     tuning,
		tel:        tel,
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	defer close(a.events)
	defer func() {
		if a.tracker != nil {
			a.tracker.SetConnected(a.chain, false)
		}
		a.tel.Metrics.ConnectionStatus.Record(context.Background(), 0,
			otelmetric.WithAttributes(attribute.String("chain", string(a.chain))))
	}()
	logger := zap.L().Named("evm")

	reconnCfg := adapter.ReconnectConfig{
		InitialBackoff: a.cfg.Backoff.Initial,
		MaxBackoff:     a.cfg.Backoff.Max,
		MaxRetries:     a.cfg.Backoff.MaxRetries,
	}

	return adapter.WithReconnect(ctx, reconnCfg, func(ctx context.Context) error {
		logger.Info("connecting to EVM RPC",
			zap.String("chain", string(a.chain)),
			zap.String("url", a.cfg.RPCURL),
			zap.String("mode", a.cfg.Mode),
		)

		strategy := a.strategy
		if strategy == nil {
			s, err := a.createStrategy().Get()
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

		if a.tracker != nil {
			a.tracker.SetConnected(a.chain, true)
		}
		a.tel.Metrics.ConnectionStatus.Record(ctx, 1,
			otelmetric.WithAttributes(attribute.String("chain", string(a.chain))))

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
					a.tel.Metrics.ReconnectCount.Add(ctx, 1,
						otelmetric.WithAttributes(attribute.String("chain", string(a.chain))))
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

func (a *Adapter) createStrategy() mo.Result[SubscriptionStrategy] {
	if a.cfg.Mode == "polling" {
		client := a.httpClient
		if client == nil {
			httpURL := DeriveHTTPURL(a.cfg.RPCURL)
			c, err := ethclient.Dial(httpURL)
			if err != nil {
				return mo.Err[SubscriptionStrategy](err)
			}
			client = c
		}
		return mo.Ok[SubscriptionStrategy](NewPollingStrategy(client, a.cfg.PollInterval, a.tuning.LogChannelBuffer))
	}
	client, err := ethclient.Dial(a.cfg.RPCURL)
	if err != nil {
		return mo.Err[SubscriptionStrategy](err)
	}
	return mo.Ok[SubscriptionStrategy](NewWebSocketStrategy(client, a.tuning.LogChannelBuffer))
}

func (a *Adapter) buildFilterQuery() ethereum.FilterQuery {
	strs := a.reg.ContractAddresses(a.chain)
	addrs := make([]common.Address, len(strs))
	for i, s := range strs {
		addrs[i] = common.HexToAddress(s)
	}
	return ethereum.FilterQuery{Addresses: addrs}
}

func (a *Adapter) processLog(ctx context.Context, logger *zap.Logger, log ethtypes.Log) {
	rawEvent, err := a.decoder.Decode(a.chain, log).Get()
	if err != nil {
		a.tel.Metrics.EventsDropped.Add(ctx, 1,
			otelmetric.WithAttributes(
				attribute.String("stage", "adapter"),
				attribute.String("chain", string(a.chain)),
			))
		logger.Debug("skipping undecoded log",
			zap.String("tx", log.TxHash.Hex()),
			zap.Error(err),
		)
		return
	}

	ts := a.cache.Get(log.BlockNumber).OrElse(0)
	if ts == 0 {
		ts = a.fetchBlockTimestamp(ctx, logger, log.BlockNumber).OrElse(0)
		if ts > 0 {
			a.cache.Set(log.BlockNumber, ts)
		}
	}
	rawEvent.Timestamp = ts

	if a.tracker != nil {
		a.tracker.UpdateBlock(a.chain, log.BlockNumber, rawEvent.Timestamp)
	}

	a.tel.Metrics.BlocksProcessed.Add(ctx, 1,
		otelmetric.WithAttributes(attribute.String("chain", string(a.chain))))

	eventCtx := ctx
	if a.tel.Config.Tracing.Stages.Adapter {
		var span trace.Span
		eventCtx, span = a.tel.Tracer.Start(ctx, "pipeline.adapter",
			trace.WithAttributes(
				attribute.String("chain", string(a.chain)),
				attribute.String("event_type", string(rawEvent.EventType)),
				attribute.Int64("block", int64(log.BlockNumber)),
			))
		span.End()
	}

	traced := pipeline.Traced[types.RawEvent]{
		Value:     rawEvent,
		Ctx:       eventCtx,
		StartedAt: time.Now(),
	}

	a.tel.Metrics.EventsReceived.Add(ctx, 1,
		otelmetric.WithAttributes(
			attribute.String("stage", "adapter"),
			attribute.String("chain", string(a.chain)),
		))

	logger.Debug("raw event received",
		zap.String("chain", string(a.chain)),
		zap.Uint64("block", log.BlockNumber),
		zap.String("tx", log.TxHash.Hex()),
		zap.String("event_type", string(rawEvent.EventType)),
	)

	select {
	case a.events <- traced:
	case <-ctx.Done():
	}
}

func (a *Adapter) fetchBlockTimestamp(ctx context.Context, logger *zap.Logger, block uint64) mo.Option[int64] {
	client := a.httpClient
	if client == nil {
		httpURL := DeriveHTTPURL(a.cfg.RPCURL)
		c, err := ethclient.DialContext(ctx, httpURL)
		if err != nil {
			logger.Warn("failed to dial for block header", zap.Error(err))
			return mo.None[int64]()
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
		return mo.None[int64]()
	}
	return mo.Some(int64(header.Time))
}

func (a *Adapter) Events() <-chan pipeline.Traced[types.RawEvent] {
	return a.events
}

func (a *Adapter) Chain() types.ChainID {
	return a.chain
}
