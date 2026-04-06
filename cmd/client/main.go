package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/vaporif/x-chain-oracle/proto"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "gRPC server address")
	signalTypes := flag.String("types", "", "comma-separated signal types to filter")
	sourceChains := flag.String("chains", "", "comma-separated source chains to filter")
	destChains := flag.String("dest-chains", "", "comma-separated destination chains to filter")
	tokens := flag.String("tokens", "", "comma-separated tokens to filter")
	minConfidence := flag.Float64("min-confidence", 0, "minimum confidence threshold")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewOracleServiceClient(conn)

	filter := &pb.SignalFilter{
		SignalTypes:       splitCSV(*signalTypes),
		SourceChains:      splitCSV(*sourceChains),
		DestinationChains: splitCSV(*destChains),
		Tokens:            splitCSV(*tokens),
		MinConfidence:     *minConfidence,
	}

	stream, err := client.SubscribeSignals(ctx, filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "subscribed to %s (filter: %+v)\n", *addr, filter)

	for {
		sig, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "shutting down")
				return
			}
			fmt.Fprintf(os.Stderr, "recv: %v\n", err)
			return
		}

		j, _ := json.MarshalIndent(sig, "", "  ")
		fmt.Println(string(j))
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
