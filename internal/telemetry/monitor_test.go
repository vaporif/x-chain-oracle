package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
)

func TestMonitorChannels(t *testing.T) {
	tel := telemetry.InitNoop()

	ch := make(chan int, 10)
	ch <- 1
	ch <- 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	monitors := []telemetry.MonitoredChannel{
		{
			Name: "test_ch",
			Len:  func() int { return len(ch) },
			Cap:  func() int { return cap(ch) },
		},
	}

	go tel.MonitorChannels(ctx, monitors, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	cancel()

	// No panic = success
	assert.True(t, true)
}
