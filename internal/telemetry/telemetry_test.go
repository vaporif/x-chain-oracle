package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
)

func TestInitNoop(t *testing.T) {
	tel := telemetry.InitNoop()
	require.NotNil(t, tel)
	require.NotNil(t, tel.Tracer)
	require.NotNil(t, tel.Metrics)

	ctx, span := tel.Tracer.Start(context.Background(), "test")
	assert.NotNil(t, ctx)
	span.End()

	tel.Metrics.EventsReceived.Add(ctx, 1)
	tel.Metrics.StageLatency.Record(ctx, 1.0)
}

func TestInitNoopShutdown(t *testing.T) {
	tel := telemetry.InitNoop()
	err := tel.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestInitWithDisabledConfig(t *testing.T) {
	cfg := config.TelemetryConfig{Enabled: false}
	tel, err := telemetry.Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	ctx, span := tel.Tracer.Start(context.Background(), "test")
	assert.NotNil(t, ctx)
	span.End()
}

func TestInitWithBadEndpoint(t *testing.T) {
	cfg := config.TelemetryConfig{
		Enabled:      true,
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		HTTPPort:     9090,
		ServiceName:  "test",
		Tracing: config.TracingConfig{
			SampleRatio:  1.0,
			BatchTimeout: 5 * time.Second,
			Stages: config.StageToggles{
				Adapter: true, Normalizer: true, Enricher: true, Engine: true, Emitter: true,
			},
		},
		Metrics: config.MetricsConfig{
			ExportInterval: 10 * time.Second,
			HistogramBuckets: config.HistogramBuckets{
				LatencyMs: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
			},
		},
	}

	// Init should succeed even with unreachable endpoint — OTel exporters connect lazily
	tel, err := telemetry.Init(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, tel)

	// Shutdown may return an error because the OTLP exporter tries to flush
	// to the unreachable endpoint — that's fine, we just verify it doesn't panic.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = tel.Shutdown(shutdownCtx)
}
