package enricher

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/price"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Enricher struct {
	reg     *registry.Registry
	pp      price.Provider
	workers int
	tel     *telemetry.Telemetry
}

func New(reg *registry.Registry, pp price.Provider, workers int, tel *telemetry.Telemetry) *Enricher {
	if workers <= 0 {
		workers = 4
	}
	return &Enricher{reg: reg, pp: pp, workers: workers, tel: tel}
}

func (e *Enricher) Run(ctx context.Context, in <-chan pipeline.Traced[types.ChainEvent], out chan<- pipeline.Traced[types.EnrichedEvent]) {
	defer close(out)

	var wg sync.WaitGroup
	for i := 0; i < e.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for traced := range in {
				if ctx.Err() != nil {
					return
				}
				result := e.processEvent(ctx, traced)
				select {
				case out <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	wg.Wait()
}

func (e *Enricher) processEvent(ctx context.Context, traced pipeline.Traced[types.ChainEvent]) pipeline.Traced[types.EnrichedEvent] {
	e.tel.Metrics.EventsReceived.Add(ctx, 1,
		otelmetric.WithAttributes(attribute.String("stage", "enricher")))

	start := time.Now()
	eventCtx := traced.Ctx
	if e.tel.Config.Tracing.Stages.Enricher {
		var span trace.Span
		eventCtx, span = e.tel.Tracer.Start(traced.Ctx, "pipeline.enricher")
		defer span.End()
	}

	enriched := e.enrich(eventCtx, traced.Value)

	e.tel.Metrics.StageLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
		otelmetric.WithAttributes(attribute.String("stage", "enricher")))
	e.tel.Metrics.EventsEmitted.Add(ctx, 1,
		otelmetric.WithAttributes(attribute.String("stage", "enricher")))

	return pipeline.Traced[types.EnrichedEvent]{Value: enriched, Ctx: eventCtx, StartedAt: traced.StartedAt}
}

func (e *Enricher) enrich(ctx context.Context, ce types.ChainEvent) types.EnrichedEvent {
	logger := zap.L().Named("enricher")
	enriched := types.EnrichedEvent{ChainEvent: ce}

	if info, ok := e.reg.LookupContract(ce.Chain, ce.ContractAddress).Get(); ok {
		enriched.ContractName = mo.Some(info.Name)
		enriched.Protocol = mo.Some(info.Protocol)
	} else {
		enriched.ContractName = mo.None[string]()
		enriched.Protocol = mo.None[string]()
	}

	result := e.pp.GetPriceUSD(ctx, ce.Token)
	if unitPrice, err := result.Get(); err == nil {
		e.tel.Metrics.PriceLookups.Add(ctx, 1,
			otelmetric.WithAttributes(attribute.String("result", "hit")))
		if amt, parseErr := strconv.ParseFloat(ce.Amount, 64); parseErr == nil && amt > 0 {
			enriched.AmountUSD = mo.Some(amt * unitPrice)
		} else {
			enriched.AmountUSD = mo.None[float64]()
		}
	} else {
		e.tel.Metrics.PriceLookups.Add(ctx, 1,
			otelmetric.WithAttributes(attribute.String("result", "miss")))
		enriched.AmountUSD = mo.None[float64]()
		logger.Debug("price unavailable", zap.String("token", ce.Token))
	}

	logger.Debug("event enriched",
		zap.String("tx", ce.TxHash),
		zap.String("contract_name", enriched.ContractName.OrElse("unknown")),
		zap.Float64("amount_usd", enriched.AmountUSD.OrElse(0)),
	)

	return enriched
}

func Enrich(ctx context.Context, ce types.ChainEvent, reg *registry.Registry, pp price.Provider) types.EnrichedEvent {
	e := &Enricher{reg: reg, pp: pp, tel: telemetry.InitNoop()}
	return e.enrich(ctx, ce)
}
