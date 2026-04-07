package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
)

func TestHTTPHealthz(t *testing.T) {
	cfg := config.TelemetryConfig{
		Enabled:      true,
		OTLPInsecure: true,
		HTTPPort:     0,
		ServiceName:  "test",
		Tracing: config.TracingConfig{
			SampleRatio:  1.0,
			BatchTimeout: 5 * time.Second,
			Stages:       config.StageToggles{Adapter: true, Normalizer: true, Enricher: true, Engine: true, Emitter: true},
		},
		Metrics: config.MetricsConfig{
			ExportInterval: 10 * time.Second,
			HistogramBuckets: config.HistogramBuckets{
				LatencyMs: []float64{1, 10, 100},
			},
		},
	}

	tel, err := telemetry.Init(context.Background(), cfg)
	require.NoError(t, err)
	defer tel.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := tel.ServeHTTP(ctx)
	require.NoError(t, err)

	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Contains(t, body, "uptime_seconds")
}

func TestHTTPMetrics(t *testing.T) {
	cfg := config.TelemetryConfig{
		Enabled:      true,
		OTLPInsecure: true,
		HTTPPort:     0,
		ServiceName:  "test",
		Tracing: config.TracingConfig{
			SampleRatio:  1.0,
			BatchTimeout: 5 * time.Second,
			Stages:       config.StageToggles{Adapter: true, Normalizer: true, Enricher: true, Engine: true, Emitter: true},
		},
		Metrics: config.MetricsConfig{
			ExportInterval: 10 * time.Second,
			HistogramBuckets: config.HistogramBuckets{
				LatencyMs: []float64{1, 10, 100},
			},
		},
	}

	tel, err := telemetry.Init(context.Background(), cfg)
	require.NoError(t, err)
	defer tel.Shutdown(context.Background())

	tel.Metrics.EventsReceived.Add(context.Background(), 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := tel.ServeHTTP(ctx)
	require.NoError(t, err)

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "oracle_events_received")
}
