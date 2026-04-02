package adapter

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type ReconnectConfig struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxRetries     int
}

func WithReconnect(ctx context.Context, cfg ReconnectConfig, connect func(ctx context.Context) error) error {
	backoff := cfg.InitialBackoff
	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := connect(ctx)
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		zap.L().Named("adapter").Warn("connection failed, retrying",
			zap.Int("attempt", attempt+1),
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)

		if attempt == cfg.MaxRetries-1 {
			return fmt.Errorf("max retries (%d) exhausted: %w", cfg.MaxRetries, err)
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}

		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}
	return fmt.Errorf("max retries exhausted")
}
