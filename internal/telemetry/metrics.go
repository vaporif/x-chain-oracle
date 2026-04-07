package telemetry

import (
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"go.opentelemetry.io/otel/metric"

	"github.com/vaporif/x-chain-oracle/internal/config"
)

type Metrics struct {
	// Pipeline throughput
	EventsReceived metric.Int64Counter
	EventsEmitted  metric.Int64Counter
	EventsDropped  metric.Int64Counter

	// Latency
	StageLatency    metric.Float64Histogram
	PipelineLatency metric.Float64Histogram

	// Enricher
	PriceLookups metric.Int64Counter

	// Engine
	RulesEvaluated   metric.Int64Counter
	RulesMatched     metric.Int64Counter
	CorrelationsOpen metric.Int64Gauge

	// Adapter
	BlocksProcessed  metric.Int64Counter
	ReconnectCount   metric.Int64Counter
	ConnectionStatus metric.Int64Gauge

	// gRPC
	ActiveSubscribers metric.Int64Gauge
	SignalsEmitted    metric.Int64Counter
	SignalsDropped    metric.Int64Counter

	// System
	ChannelUtilization metric.Float64Gauge
}

func newMetrics(meter metric.Meter, buckets config.HistogramBuckets) (*Metrics, error) {
	m := &Metrics{}
	var err error

	if m.EventsReceived, err = meter.Int64Counter("oracle.events.received"); err != nil {
		return nil, err
	}
	if m.EventsEmitted, err = meter.Int64Counter("oracle.events.emitted"); err != nil {
		return nil, err
	}
	if m.EventsDropped, err = meter.Int64Counter("oracle.events.dropped"); err != nil {
		return nil, err
	}
	if m.StageLatency, err = meter.Float64Histogram("oracle.stage.latency",
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(buckets.LatencyMs...),
	); err != nil {
		return nil, err
	}
	if m.PipelineLatency, err = meter.Float64Histogram("oracle.pipeline.latency",
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(buckets.LatencyMs...),
	); err != nil {
		return nil, err
	}
	if m.PriceLookups, err = meter.Int64Counter("oracle.enricher.price_lookups"); err != nil {
		return nil, err
	}
	if m.RulesEvaluated, err = meter.Int64Counter("oracle.engine.rules_evaluated"); err != nil {
		return nil, err
	}
	if m.RulesMatched, err = meter.Int64Counter("oracle.engine.rules_matched"); err != nil {
		return nil, err
	}
	if m.CorrelationsOpen, err = meter.Int64Gauge("oracle.engine.correlations_open"); err != nil {
		return nil, err
	}
	if m.BlocksProcessed, err = meter.Int64Counter("oracle.adapter.blocks_processed"); err != nil {
		return nil, err
	}
	if m.ReconnectCount, err = meter.Int64Counter("oracle.adapter.reconnect_count"); err != nil {
		return nil, err
	}
	if m.ConnectionStatus, err = meter.Int64Gauge("oracle.adapter.connection_status"); err != nil {
		return nil, err
	}
	if m.ActiveSubscribers, err = meter.Int64Gauge("oracle.grpc.active_subscribers"); err != nil {
		return nil, err
	}
	if m.SignalsEmitted, err = meter.Int64Counter("oracle.grpc.signals_emitted"); err != nil {
		return nil, err
	}
	if m.SignalsDropped, err = meter.Int64Counter("oracle.grpc.signals_dropped"); err != nil {
		return nil, err
	}
	if m.ChannelUtilization, err = meter.Float64Gauge("oracle.pipeline.channel_utilization"); err != nil {
		return nil, err
	}
	return m, nil
}

func newNoopMetrics(meter metric.Meter) *Metrics {
	npm := noopmetric.NewMeterProvider().Meter("noop")
	m, _ := newMetrics(npm, config.HistogramBuckets{
		LatencyMs: []float64{1},
		AmountUSD: []float64{1},
	})
	_ = meter
	return m
}
