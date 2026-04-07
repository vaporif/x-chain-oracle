package telemetry

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

func (t *Telemetry) GRPCStatsHandler() stats.Handler {
	if t.tracerProvider == nil && t.meterProvider == nil {
		return nil
	}

	var opts []otelgrpc.Option
	if t.tracerProvider != nil {
		opts = append(opts, otelgrpc.WithTracerProvider(t.tracerProvider))
	}
	if t.meterProvider != nil {
		opts = append(opts, otelgrpc.WithMeterProvider(t.meterProvider))
	}
	return otelgrpc.NewServerHandler(opts...)
}
