package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	grpcemitter "github.com/vaporif/x-chain-oracle/internal/signal/grpc"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
	pb "github.com/vaporif/x-chain-oracle/proto"
)

const bufSize = 1024 * 1024

func setupServer(t *testing.T) (*grpcemitter.Emitter, pb.OracleServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	emitter := grpcemitter.NewEmitter(50051, 64, nil, telemetry.InitNoop())

	srv := grpc.NewServer()
	pb.RegisterOracleServiceServer(srv, emitter)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := pb.NewOracleServiceClient(conn)
	cleanup := func() {
		_ = conn.Close()
		srv.GracefulStop()
		_ = lis.Close()
	}
	return emitter, client, cleanup
}

func TestSubscribeReceivesMatchingSignals(t *testing.T) {
	emitter, client, cleanup := setupServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.SubscribeSignals(ctx, &pb.SignalFilter{
		SignalTypes: []string{"liquidity_needed"},
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = emitter.Emit(ctx, types.Signal{
		ID:               "sig-1",
		SignalType:       "liquidity_needed",
		SourceChain:      types.ChainEthereum,
		DestinationChain: mo.Some(types.ChainID("solana")),
		Token:            "USDC",
		Amount:           "1000000",
		AmountUSD:        mo.Some(1000.0),
		DetectedAt:       time.Now().Unix(),
		Confidence:       0.8,
		Metadata:         map[string]string{"key": "val"},
	})
	require.NoError(t, err)

	msg, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "sig-1", msg.Id)
	assert.Equal(t, "liquidity_needed", msg.SignalType)
	assert.Equal(t, "solana", msg.DestinationChain)
}

func TestSubscribeFiltersNonMatching(t *testing.T) {
	emitter, client, cleanup := setupServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.SubscribeSignals(ctx, &pb.SignalFilter{
		SignalTypes: []string{"liquidity_needed"},
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = emitter.Emit(ctx, types.Signal{
		ID:         "sig-2",
		SignalType: "burst_detected",
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		_, recvErr := stream.Recv()
		if recvErr != nil {
			close(done)
		}
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
	}
}
