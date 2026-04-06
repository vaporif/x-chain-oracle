package evm

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

type PollingStrategy struct {
	client   *ethclient.Client
	interval time.Duration
}

func NewPollingStrategy(client *ethclient.Client, interval time.Duration) *PollingStrategy {
	if interval <= 0 {
		interval = 12 * time.Second
	}
	return &PollingStrategy{client: client, interval: interval}
}

func (s *PollingStrategy) Subscribe(ctx context.Context, filter ethereum.FilterQuery) (<-chan types.Log, ethereum.Subscription, error) {
	logs := make(chan types.Log, 256)
	sub := &pollingSub{
		errCh:  make(chan error, 1),
		cancel: func() {},
	}

	pollCtx, cancel := context.WithCancel(ctx)
	sub.cancel = cancel

	header, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	lastBlock := header.Number.Uint64()

	go func() {
		defer close(logs)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		logger := zap.L().Named("evm.polling")

		for {
			select {
			case <-ticker.C:
				currentHeader, err := s.client.HeaderByNumber(pollCtx, nil)
				if err != nil {
					logger.Warn("failed to get latest block", zap.Error(err))
					continue
				}
				currentBlock := currentHeader.Number.Uint64()
				if currentBlock <= lastBlock {
					continue
				}

				query := filter
				query.FromBlock = new(big.Int).SetUint64(lastBlock + 1)
				query.ToBlock = new(big.Int).SetUint64(currentBlock)

				results, err := s.client.FilterLogs(pollCtx, query)
				if err != nil {
					logger.Warn("FilterLogs failed", zap.Error(err))
					continue
				}

				for _, l := range results {
					select {
					case logs <- l:
					case <-pollCtx.Done():
						return
					}
				}
				lastBlock = currentBlock

			case <-pollCtx.Done():
				return
			}
		}
	}()

	return logs, sub, nil
}

type pollingSub struct {
	once   sync.Once
	cancel context.CancelFunc
	errCh  chan error
}

func (s *pollingSub) Err() <-chan error {
	return s.errCh
}

func (s *pollingSub) Unsubscribe() {
	s.once.Do(func() {
		s.cancel()
		close(s.errCh)
	})
}
