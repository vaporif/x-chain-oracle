package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"slices"

	"go.uber.org/zap"
	googlegrpc "google.golang.org/grpc"

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
	mu                sync.RWMutex
	listeners         []*listener
	dropped           atomic.Int64
	emitted           atomic.Int64
	server            *googlegrpc.Server
}

func NewEmitter(port int, subscriberBufSize int) *Emitter {
	return &Emitter{port: port, subscriberBufSize: subscriberBufSize}
}

func (e *Emitter) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", e.port))
	if err != nil {
		return err
	}
	e.server = googlegrpc.NewServer()
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
			zap.L().Named("grpc").Debug("signal emitted",
				zap.String("signal_id", pbSig.Id),
				zap.String("signal_type", pbSig.SignalType),
			)
		default:
			e.dropped.Add(1)
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
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.listeners = slices.DeleteFunc(e.listeners, func(existing *listener) bool {
			return existing == l
		})
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
	return &pb.StatusResponse{
		SignalsEmitted: e.emitted.Load(),
	}, nil
}

func (e *Emitter) Run(ctx context.Context, signals <-chan types.Signal) {
	for sig := range signals {
		if ctx.Err() != nil {
			return
		}
		if err := e.Emit(ctx, sig); err != nil {
			zap.L().Named("grpc").Error("emit failed", zap.Error(err))
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
