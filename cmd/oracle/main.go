package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/adapter"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/price/chainlink"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	grpcemitter "github.com/vaporif/x-chain-oracle/internal/signal/grpc"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func main() {
	zapLogger, _ := zap.NewProduction()
	defer zapLogger.Sync()
	zap.ReplaceGlobals(zapLogger)
	logger := zapLogger.Named("main")

	cfg, err := config.Load("config/config.toml")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	reg, err := registry.Load("config/registry.toml")
	if err != nil {
		log.Fatalf("registry: %v", err)
	}
	rules, err := engine.LoadRules("config/rules.toml")
	if err != nil {
		log.Fatalf("rules: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	priceProvider := chainlink.New(cfg.Chainlink)
	emitter := grpcemitter.NewEmitter(cfg.GRPC.Port)

	rawEvents := make(chan types.RawEvent, 512)
	chainEvents := make(chan types.ChainEvent, 256)
	enrichedEvents := make(chan types.EnrichedEvent, 64)
	signals := make(chan types.Signal, 32)

	var adapters []adapter.ChainAdapter
	if ethCfg, ok := cfg.Chains["ethereum"]; ok {
		adapters = append(adapters, evm.New(types.ChainEthereum, ethCfg, reg))
	}

	var wg sync.WaitGroup
	var adapterWg sync.WaitGroup

	for _, a := range adapters {
		adapterWg.Add(1)
		wg.Add(1)
		go func(a adapter.ChainAdapter) {
			defer wg.Done()
			go func() {
				defer adapterWg.Done()
				for event := range a.Events() {
					rawEvents <- event
				}
			}()
			if err := a.Start(ctx); err != nil && ctx.Err() == nil {
				logger.Error("adapter failed",
					zap.String("chain", string(a.Chain())),
					zap.Error(err),
				)
			}
		}(a)
	}

	go func() {
		adapterWg.Wait()
		close(rawEvents)
	}()

	enr := enricher.New(reg, priceProvider, cfg.Enricher.Workers)
	eng := engine.New(rules)

	wg.Add(4)
	go func() { defer wg.Done(); normalizer.Run(ctx, rawEvents, chainEvents) }()
	go func() { defer wg.Done(); enr.Run(ctx, chainEvents, enrichedEvents) }()
	go func() { defer wg.Done(); eng.Run(ctx, enrichedEvents, signals) }()
	go func() { defer wg.Done(); emitter.Run(ctx, signals) }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := emitter.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Fatal("gRPC server failed", zap.Error(err))
		}
	}()

	logger.Info("oracle started", zap.Int("adapters", len(adapters)), zap.Int("grpc_port", cfg.GRPC.Port))

	<-ctx.Done()
	logger.Info("shutting down")
	cancel()
	wg.Wait()
	logger.Info("shutdown complete")
}
