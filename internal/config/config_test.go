package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/config"
)

func TestLoadConfig(t *testing.T) {
	toml := `
[grpc]
port = 50051

[enricher]
workers = 4

[chainlink]
cache_ttl = "30s"

[chains.ethereum]
rpc_url = "wss://eth.example.com"
mode = "websocket"

[chains.solana]
rpc_url = "wss://sol.example.com"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 50051, cfg.GRPC.Port)
	assert.Equal(t, 4, cfg.Enricher.Workers)
	assert.Equal(t, "wss://eth.example.com", cfg.Chains["ethereum"].RPCURL)
	assert.Equal(t, "websocket", cfg.Chains["ethereum"].Mode)
}

func TestLoadConfigWithLogLevel(t *testing.T) {
	toml := `
log_level = "debug"

[grpc]
port = 50051

[enricher]
workers = 4

[chainlink]
cache_ttl = "30s"

[chains.ethereum]
rpc_url = "wss://eth.example.com"
mode = "websocket"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestEnvVarOverride(t *testing.T) {
	toml := `
[grpc]
port = 50051

[enricher]
workers = 4

[chainlink]
cache_ttl = "30s"

[chains.ethereum]
rpc_url = "wss://default.example.com"
mode = "websocket"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	t.Setenv("ORACLE_CHAINS_ETHEREUM_RPC_URL", "wss://override.example.com")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "wss://override.example.com", cfg.Chains["ethereum"].RPCURL)
}
