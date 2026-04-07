//go:build e2e

package e2e_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/engine"
	"github.com/vaporif/x-chain-oracle/internal/enricher"
	"github.com/vaporif/x-chain-oracle/internal/normalizer"
	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/price/chainlink"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	grpcemitter "github.com/vaporif/x-chain-oracle/internal/signal/grpc"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	otypes "github.com/vaporif/x-chain-oracle/internal/types"
	pb "github.com/vaporif/x-chain-oracle/proto"
)

const anvilPrivateKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
const testTokenAddr = "0x1111111111111111111111111111111111111111"
const wormholeTokenBridge = "0x3ee18B2214AFF97000D974cf647E7C347E8fa585"

func startAnvil(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/foundry-rs/foundry:latest",
		ExposedPorts: []string{"8545/tcp"},
		Entrypoint:   []string{"anvil", "--host", "0.0.0.0"},
		WaitingFor:   wait.ForListeningPort("8545/tcp").WithStartupTimeout(30 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "8545")
	require.NoError(t, err)

	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

var deployedAddrRe = regexp.MustCompile(`Deployed to:\s+(0x[0-9a-fA-F]{40})`)

func forgeDeploy(t *testing.T, rpcURL, contractPath, contractName string, constructorArgs ...string) string {
	t.Helper()
	solcPath, err := exec.LookPath("solc")
	require.NoError(t, err, "solc not found in PATH")

	args := []string{
		"create", contractPath + ":" + contractName,
		"--rpc-url", rpcURL,
		"--private-key", anvilPrivateKey,
		"--use", solcPath,
		"--broadcast",
	}
	if len(constructorArgs) > 0 {
		args = append(args, "--constructor-args")
		args = append(args, constructorArgs...)
	}
	cmd := exec.Command("forge", args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "forge create failed: %s", string(out))

	matches := deployedAddrRe.FindSubmatch(out)
	require.NotEmpty(t, matches, "could not find deployed address in forge output: %s", string(out))
	return string(matches[1])
}

func writeTestConfig(t *testing.T, dir string, rpcURL string, grpcPort int) string {
	t.Helper()
	content := fmt.Sprintf(`
log_level = "debug"

[grpc]
port = %d
subscriber_buffer_size = 64

[enricher]
workers = 2

[chainlink]
cache_ttl = "30s"
staleness_threshold = "2h"

[engine]
default_window_ttl = "10s"
prune_interval = "2s"
max_window_size = 1000

[chains.ethereum]
rpc_url = "%s"
mode = "polling"
poll_interval = "200ms"
event_buffer = 256
`, grpcPort, rpcURL)
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func writeTestRegistry(t *testing.T, dir, wormholeAddr, chainlinkAddr string) string {
	t.Helper()
	content := fmt.Sprintf(`
[contracts.ethereum."%s"]
name = "Mock Wormhole Core"
protocol = "wormhole"
median_bridge_latency = "15m"

[price_feeds.ethereum."%s"]
address = "%s"

[wormhole.ethereum]
core = "%s"
token_bridge = "%s"
`, wormholeAddr, strings.ToLower(testTokenAddr), chainlinkAddr, wormholeAddr, wormholeTokenBridge)
	path := filepath.Join(dir, "registry.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func writeTestRules(t *testing.T, dir string) string {
	t.Helper()
	content := `
[[rules]]
name = "large_bridge_deposit_solana"
trigger = "bridge_deposit"
signal = "liquidity_needed"
confidence = 0.8
metadata_fields = ["destination_chain", "token", "amount"]

[[rules.conditions]]
field = "amount_usd"
op = "gt"
value = "50000"

[[rules.conditions]]
field = "destination_chain"
op = "eq"
value = "solana"
`
	path := filepath.Join(dir, "rules.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func buildWormholeTransferPayload(amount *big.Int, tokenAddr common.Address, tokenChain, recipientChain uint16) []byte {
	payload := make([]byte, 133)
	payload[0] = 1
	amountBytes := amount.Bytes()
	copy(payload[1+32-len(amountBytes):33], amountBytes)
	copy(payload[33+12:65], tokenAddr.Bytes())
	binary.BigEndian.PutUint16(payload[65:67], tokenChain)
	binary.BigEndian.PutUint16(payload[99:101], recipientChain)
	return payload
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestE2EPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	zapLogger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(zapLogger)

	// Start Anvil
	rpcURL := startAnvil(t, ctx)
	t.Logf("Anvil running at %s", rpcURL)

	// Deploy contracts
	chainlinkAddr := forgeDeploy(t, rpcURL,
		"contracts/test/MockChainlinkAggregator.sol", "MockChainlinkAggregator",
		"100000000", "8") // $1.00 with 8 decimals
	t.Logf("MockChainlinkAggregator deployed at %s", chainlinkAddr)

	wormholeAddr := forgeDeploy(t, rpcURL,
		"contracts/test/MockWormholeCore.sol", "MockWormholeCore",
		wormholeTokenBridge)
	t.Logf("MockWormholeCore deployed at %s", wormholeAddr)

	// Write temp config files
	tmpDir := t.TempDir()
	grpcPort := freePort(t)

	configPath := writeTestConfig(t, tmpDir, rpcURL, grpcPort)
	registryPath := writeTestRegistry(t, tmpDir, wormholeAddr, chainlinkAddr)
	rulesPath := writeTestRules(t, tmpDir)

	// Load configs
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	reg, err := registry.Load(registryPath)
	require.NoError(t, err)

	rules, err := engine.LoadRules(rulesPath)
	require.NoError(t, err)

	// Shared ethclient for both adapter and chainlink
	client, err := ethclient.DialContext(ctx, rpcURL)
	require.NoError(t, err)
	defer client.Close()

	// Build pipeline components
	tel := telemetry.InitNoop()
	priceProvider := chainlink.New(cfg.Chainlink, client, reg, otypes.ChainEthereum)
	emitter := grpcemitter.NewEmitter(grpcPort, cfg.GRPC.SubscriberBufferSize, nil, tel)

	rawEvents := make(chan pipeline.Traced[otypes.RawEvent], 256)
	chainEvents := make(chan pipeline.Traced[otypes.ChainEvent], 256)
	enrichedEvents := make(chan pipeline.Traced[otypes.EnrichedEvent], 64)
	signals := make(chan pipeline.Traced[otypes.Signal], 32)

	adapter := evm.New(otypes.ChainEthereum, cfg.Chains["ethereum"], reg, nil, client, nil, cfg.Tuning, tel)
	enr := enricher.New(reg, priceProvider, cfg.Enricher.Workers, tel)
	eng := engine.New(rules, engine.CorrelatorConfig{
		DefaultWindowTTL: cfg.Engine.DefaultWindowTTL,
		PruneInterval:    cfg.Engine.PruneInterval,
		MaxWindowSize:    cfg.Engine.MaxWindowSize,
	}, tel)

	// Start pipeline goroutines
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = adapter.Start(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for event := range adapter.Events() {
			rawEvents <- event
		}
		close(rawEvents)
	}()

	wg.Add(4)
	go func() { defer wg.Done(); normalizer.Run(ctx, tel, rawEvents, chainEvents) }()
	go func() { defer wg.Done(); enr.Run(ctx, chainEvents, enrichedEvents) }()
	go func() { defer wg.Done(); eng.Run(ctx, enrichedEvents, signals) }()
	go func() { defer wg.Done(); emitter.Run(ctx, signals) }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = emitter.Start(ctx)
	}()

	// Give gRPC server a moment to bind
	time.Sleep(500 * time.Millisecond)

	// Connect gRPC client
	conn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	grpcClient := pb.NewOracleServiceClient(conn)
	stream, err := grpcClient.SubscribeSignals(ctx, &pb.SignalFilter{
		SignalTypes: []string{"liquidity_needed"},
	})
	require.NoError(t, err)

	// Send a transaction to MockWormholeCore.publishMessage(payload)
	privateKey, err := crypto.HexToECDSA(anvilPrivateKey)
	require.NoError(t, err)

	tokenAddr := common.HexToAddress(testTokenAddr)
	amount := new(big.Int).SetUint64(60_000_000_000) // 60000 * $1.00 = $60,000 USD
	payload := buildWormholeTransferPayload(amount, tokenAddr, 2, 1)

	// ABI encode: publishMessage(bytes)
	selector := crypto.Keccak256([]byte("publishMessage(bytes)"))[:4]
	offset := make([]byte, 32)
	offset[31] = 32
	length := make([]byte, 32)
	binary.BigEndian.PutUint64(length[24:], uint64(len(payload)))
	paddedPayload := make([]byte, ((len(payload)+31)/32)*32)
	copy(paddedPayload, payload)

	var calldata []byte
	calldata = append(calldata, selector...)
	calldata = append(calldata, offset...)
	calldata = append(calldata, length...)
	calldata = append(calldata, paddedPayload...)

	nonce, err := client.PendingNonceAt(ctx, crypto.PubkeyToAddress(privateKey.PublicKey))
	require.NoError(t, err)

	chainID, err := client.ChainID(ctx)
	require.NoError(t, err)

	wormholeContractAddr := common.HexToAddress(wormholeAddr)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(30_000_000_000),
		Gas:       500_000,
		To:        &wormholeContractAddr,
		Data:      calldata,
	})

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)
	t.Logf("Transaction sent: %s", signedTx.Hash().Hex())

	// Assert gRPC signal arrives
	type result struct {
		sig *pb.Signal
		err error
	}
	ch := make(chan result, 1)
	go func() {
		sig, err := stream.Recv()
		ch <- result{sig, err}
	}()

	sigCtx, sigCancel := context.WithTimeout(ctx, 15*time.Second)
	defer sigCancel()

	select {
	case r := <-ch:
		require.NoError(t, r.err)
		sig := r.sig
		assert.Equal(t, "liquidity_needed", sig.SignalType)
		assert.Equal(t, "ethereum", sig.SourceChain)
		assert.Equal(t, "solana", sig.DestinationChain)
		assert.Equal(t, 0.8, sig.Confidence)
		assert.Greater(t, sig.AmountUsd, float64(50000))
		t.Logf("Signal received: type=%s source=%s dest=%s amount_usd=%.2f confidence=%.2f",
			sig.SignalType, sig.SourceChain, sig.DestinationChain, sig.AmountUsd, sig.Confidence)
	case <-sigCtx.Done():
		t.Fatal("timed out waiting for gRPC signal")
	}

	cancel()
	wg.Wait()
}
