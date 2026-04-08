package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"slices"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	googlegrpc "google.golang.org/grpc"

	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/status"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
	pb "github.com/vaporif/x-chain-oracle/proto"
)

type listener struct {
	filter *pb.SignalFilter
	ch     chan *pb.Signal
}

type Emitter struct {
	pb.UnimplementedOracleServiceServer

	port              int
	subscriberBufSize int
	tracker           *status.Tracker
	tel               *telemetry.Telemetry
	mu                sync.RWMutex
	listeners         []*listener
	dropped           atomic.Int64
	emitted           atomic.Int64
	server            *googlegrpc.Server
}

func NewEmitter(port int, subscriberBufSize int, tracker *status.Tracker, tel *telemetry.Telemetry) *Emitter {
	return &Emitter{port: port, subscriberBufSize: subscriberBufSize, tracker: tracker, tel: tel}
}

func (e *Emitter) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", e.port))
	if err != nil {
		return err
	}

	var opts []googlegrpc.ServerOption
	if handler := e.tel.GRPCStatsHandler(); handler != nil {
		opts = append(opts, googlegrpc.StatsHandler(handler))
	}

	e.server = googlegrpc.NewServer(opts...)
	pb.RegisterOracleServiceServer(e.server, e)

	go func() {
		<-ctx.Done()
		e.server.GracefulStop()
	}()

	return e.server.Serve(lis)
}

func (e *Emitter) Stop(_ context.Context) error {
	if e.server != nil {
		e.server.GracefulStop()
	}
	return nil
}

func (e *Emitter) Emit(_ context.Context, sig types.Signal) error {
	pbSig := toProto(sig)
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, l := range e.listeners {
		if !matchesFilter(l.filter, pbSig) {
			continue
		}
		select {
		case l.ch <- pbSig:
			e.emitted.Add(1)
			e.tel.Metrics.SignalsEmitted.Add(context.Background(), 1)
			zap.L().Named("grpc").Debug("signal emitted",
				zap.String("signal_id", pbSig.Id),
				zap.String("signal_type", pbSig.SignalType),
			)
		default:
			e.dropped.Add(1)
			e.tel.Metrics.SignalsDropped.Add(context.Background(), 1)
			zap.L().Named("grpc").Debug("dropped signal for slow consumer")
		}
	}
	return nil
}

func (e *Emitter) SubscribeSignals(filter *pb.SignalFilter, stream pb.OracleService_SubscribeSignalsServer) error {
	l := &listener{
		filter: filter,
		ch:     make(chan *pb.Signal, e.subscriberBufSize),
	}

	e.mu.Lock()
	e.listeners = append(e.listeners, l)
	e.tel.Metrics.ActiveSubscribers.Record(context.Background(), int64(len(e.listeners)))
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.listeners = slices.DeleteFunc(e.listeners, func(existing *listener) bool {
			return existing == l
		})
		e.tel.Metrics.ActiveSubscribers.Record(context.Background(), int64(len(e.listeners)))
		e.mu.Unlock()
	}()

	for {
		select {
		case sig := <-l.ch:
			if err := stream.Send(sig); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (e *Emitter) GetStatus(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	resp := &pb.StatusResponse{
		SignalsEmitted: e.emitted.Load(),
	}
	if e.tracker != nil {
		chains, uptime := e.tracker.Snapshot()
		resp.UptimeSeconds = int64(uptime.Seconds())
		for chain, state := range chains {
			resp.Chains = append(resp.Chains, &pb.ChainStatus{
				ChainId:     string(chain),
				Connected:   state.Connected,
				LastBlock:   state.LastBlock,
				LastEventAt: state.LastEventAt,
			})
		}
	}
	return resp, nil
}

func (e *Emitter) Run(ctx context.Context, signals <-chan pipeline.Traced[types.Signal]) {
	for traced := range signals {
		if ctx.Err() != nil {
			return
		}

		e.tel.Metrics.EventsReceived.Add(ctx, 1,
			telemetry.StageAttr(telemetry.StageEmitter))

		var span trace.Span
		emitCtx := traced.Ctx
		if e.tel.Config.Tracing.Stages.Emitter {
			emitCtx, span = e.tel.Tracer.Start(traced.Ctx, "pipeline.emitter")
		}

		if err := e.Emit(emitCtx, traced.Value); err != nil {
			zap.L().Named("grpc").Error("emit failed", zap.Error(err))
		}

		e.tel.Metrics.PipelineLatency.Record(ctx,
			float64(time.Since(traced.StartedAt).Milliseconds()),
			otelmetric.WithAttributes(attribute.String("signal_type", traced.Value.SignalType)))

		if span != nil {
			span.End()
		}
	}
}

func toProto(sig types.Signal) *pb.Signal {
	p := &pb.Signal{
		Id:          sig.ID,
		SignalType:  sig.SignalType,
		SourceChain: string(sig.SourceChain),
		Token:       sig.Token,
		Amount:      sig.Amount,
		DetectedAt:  sig.DetectedAt,
		Confidence:  sig.Confidence,
		Metadata:    sig.Metadata,
	}
	if v, ok := sig.DestinationChain.Get(); ok {
		p.DestinationChain = string(v)
	}
	if v, ok := sig.AmountUSD.Get(); ok {
		p.AmountUsd = v
	}
	if v, ok := sig.EstimatedActionTime.Get(); ok {
		p.EstimatedActionTime = v
	}
	return p
}

func matchesFilter(f *pb.SignalFilter, sig *pb.Signal) bool {
	if len(f.SignalTypes) > 0 && !slices.Contains(f.SignalTypes, sig.SignalType) {
		return false
	}
	if len(f.SourceChains) > 0 && !slices.Contains(f.SourceChains, sig.SourceChain) {
		return false
	}
	if len(f.DestinationChains) > 0 && !slices.Contains(f.DestinationChains, sig.DestinationChain) {
		return false
	}
	if len(f.Tokens) > 0 && !slices.Contains(f.Tokens, sig.Token) {
		return false
	}
	if f.MinConfidence > 0 && sig.Confidence < f.MinConfidence {
		return false
	}
	return true
}
