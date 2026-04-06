package enricher

import (
	"context"
	"strconv"
	"sync"

	"github.com/samber/mo"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/price"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func Enrich(ctx context.Context, ce types.ChainEvent, reg *registry.Registry, pp price.Provider) types.EnrichedEvent {
	logger := zap.L().Named("enricher")
	enriched := types.EnrichedEvent{ChainEvent: ce}

	if info, ok := reg.LookupContract(ce.Chain, ce.ContractAddress); ok {
		enriched.ContractName = mo.Some(info.Name)
		enriched.Protocol = mo.Some(info.Protocol)
	} else {
		enriched.ContractName = mo.None[string]()
		enriched.Protocol = mo.None[string]()
	}

	result := pp.GetPriceUSD(ctx, ce.Token)
	if unitPrice, err := result.Get(); err == nil {
		if amt, parseErr := strconv.ParseFloat(ce.Amount, 64); parseErr == nil && amt > 0 {
			enriched.AmountUSD = mo.Some(amt * unitPrice)
		} else {
			enriched.AmountUSD = mo.None[float64]()
		}
	} else {
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

type Enricher struct {
	reg     *registry.Registry
	pp      price.Provider
	workers int
}

func New(reg *registry.Registry, pp price.Provider, workers int) *Enricher {
	if workers <= 0 {
		workers = 4
	}
	return &Enricher{reg: reg, pp: pp, workers: workers}
}

func (e *Enricher) Run(ctx context.Context, in <-chan types.ChainEvent, out chan<- types.EnrichedEvent) {
	defer close(out)

	var wg sync.WaitGroup
	for i := 0; i < e.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ce := range in {
				if ctx.Err() != nil {
					return
				}
				enriched := Enrich(ctx, ce, e.reg, e.pp)
				select {
				case out <- enriched:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	wg.Wait()
}
