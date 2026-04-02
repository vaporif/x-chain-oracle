package adapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter"
)

func TestWithReconnectSucceedsFirstTry(t *testing.T) {
	cfg := adapter.ReconnectConfig{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		MaxRetries:     3,
	}

	calls := 0
	err := adapter.WithReconnect(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestWithReconnectRetriesOnError(t *testing.T) {
	cfg := adapter.ReconnectConfig{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		MaxRetries:     3,
	}

	calls := 0
	err := adapter.WithReconnect(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("connection failed")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestWithReconnectExhaustsRetries(t *testing.T) {
	cfg := adapter.ReconnectConfig{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		MaxRetries:     2,
	}

	err := adapter.WithReconnect(context.Background(), cfg, func(ctx context.Context) error {
		return errors.New("always fails")
	})
	assert.Error(t, err)
}

func TestWithReconnectRespectsContext(t *testing.T) {
	cfg := adapter.ReconnectConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		MaxRetries:     10,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.WithReconnect(ctx, cfg, func(ctx context.Context) error {
		return errors.New("fail")
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}
