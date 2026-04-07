package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/vaporif/x-chain-oracle/internal/config"
)

type Telemetry struct {
	Tracer         trace.Tracer
	Metrics        *Metrics
	Config         config.TelemetryConfig
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *metric.MeterProvider
	promExporter   *prometheus.Exporter
}

func Init(ctx context.Context, cfg config.TelemetryConfig) (*Telemetry, error) {
	if !cfg.Enabled {
		return InitNoop(), nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	var dialOpts []grpc.DialOption
	if cfg.OTLPInsecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Trace provider
	var tp *sdktrace.TracerProvider
	if cfg.OTLPEndpoint != "" {
		traceExporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithDialOption(dialOpts...),
		)
		if err != nil {
			return nil, err
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter,
				sdktrace.WithBatchTimeout(cfg.Tracing.BatchTimeout)),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.Tracing.SampleRatio)),
			sdktrace.WithResource(res),
		)
	} else {
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.NeverSample()),
			sdktrace.WithResource(res),
		)
	}

	// Metric provider — dual readers: OTLP push + Prometheus pull
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, err
	}

	readers := []metric.Option{
		metric.WithReader(promExporter),
	}

	if cfg.OTLPEndpoint != "" {
		metricExporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithDialOption(dialOpts...),
		)
		if err != nil {
			return nil, err
		}
		readers = append(readers, metric.WithReader(
			metric.NewPeriodicReader(metricExporter,
				metric.WithInterval(cfg.Metrics.ExportInterval)),
		))
	}

	readers = append(readers, metric.WithResource(res))
	mp := metric.NewMeterProvider(readers...)

	meter := mp.Meter(cfg.ServiceName)
	metrics, err := newMetrics(meter, cfg.Metrics.HistogramBuckets)
	if err != nil {
		return nil, err
	}

	return &Telemetry{
		Tracer:         tp.Tracer(cfg.ServiceName),
		Metrics:        metrics,
		Config:         cfg,
		tracerProvider: tp,
		meterProvider:  mp,
		promExporter:   promExporter,
	}, nil
}

func InitNoop() *Telemetry {
	tp := nooptrace.NewTracerProvider()
	return &Telemetry{
		Tracer:  tp.Tracer("noop"),
		Metrics: newNoopMetrics(),
		Config:  config.TelemetryConfig{},
	}
}

func NewForTest(tracer trace.Tracer, metrics *Metrics, cfg config.TelemetryConfig) *Telemetry {
	return &Telemetry{
		Tracer:  tracer,
		Metrics: metrics,
		Config:  cfg,
	}
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.tracerProvider != nil {
		if err := t.tracerProvider.Shutdown(ctx); err != nil {
			return err
		}
	}
	if t.meterProvider != nil {
		if err := t.meterProvider.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}
