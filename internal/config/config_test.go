package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"port too high", func(c *config.Config) { c.GRPC.Port = 70000 }},
		{"port zero", func(c *config.Config) { c.GRPC.Port = 0 }},
		{"workers zero", func(c *config.Config) { c.Enricher.Workers = 0 }},
		{"cache_ttl zero", func(c *config.Config) { c.Chainlink.CacheTTL = 0 }},
		{"staleness zero", func(c *config.Config) { c.Chainlink.StalenessThreshold = 0 }},
		{"poll_interval zero", func(c *config.Config) {
			c.Chains["ethereum"] = config.ChainConfig{RPCURL: "wss://x", PollInterval: 0}
		}},
		{"max_window_size zero", func(c *config.Config) { c.Engine.MaxWindowSize = 0 }},
		{"prune_interval zero", func(c *config.Config) { c.Engine.PruneInterval = 0 }},
		{"empty rpc_url", func(c *config.Config) {
			c.Chains["ethereum"] = config.ChainConfig{RPCURL: ""}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			assert.Error(t, cfg.Validate(), "expected validation error for %s", tt.name)
		})
	}
}

func TestDefaultsApplied(t *testing.T) {
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
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(toml), 0644))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Hour, cfg.Chainlink.StalenessThreshold)
	assert.Equal(t, 64, cfg.GRPC.SubscriberBufferSize)
	assert.Equal(t, 30*time.Second, cfg.Engine.DefaultWindowTTL)
	assert.Equal(t, 5*time.Second, cfg.Engine.PruneInterval)
	assert.Equal(t, 10000, cfg.Engine.MaxWindowSize)

	eth := cfg.Chains["ethereum"]
	assert.Equal(t, 12*time.Second, eth.PollInterval)
	assert.Equal(t, 256, eth.EventBuffer)
	assert.Equal(t, 1*time.Second, eth.Backoff.Initial)
	assert.Equal(t, 30*time.Second, eth.Backoff.Max)
	assert.Equal(t, 10, eth.Backoff.MaxRetries)
}

func validConfig() config.Config {
	return config.Config{
		LogLevel: "info",
		GRPC:     config.GRPCConfig{Port: 50051, SubscriberBufferSize: 64},
		Enricher: config.EnricherConfig{Workers: 4},
		Chainlink: config.ChainlinkConfig{
			CacheTTL:           30 * time.Second,
			StalenessThreshold: 2 * time.Hour,
		},
		Engine: config.EngineConfig{
			DefaultWindowTTL: 30 * time.Second,
			PruneInterval:    5 * time.Second,
			MaxWindowSize:    10000,
		},
		Chains: map[string]config.ChainConfig{
			"ethereum": {
				RPCURL:       "wss://eth.example.com",
				Mode:         "websocket",
				PollInterval: 12 * time.Second,
				EventBuffer:  256,
				Backoff:      config.BackoffConfig{Initial: time.Second, Max: 30 * time.Second, MaxRetries: 10},
			},
		},
	}
}

func fullToml() string {
	return `
[grpc]
port = 50051

[enricher]
workers = 4

[chainlink]
cache_ttl = "30s"
staleness_threshold = "2h"

[engine]
default_window_ttl = "30s"
prune_interval = "5s"
max_window_size = 10000

[chains.ethereum]
rpc_url = "wss://eth.example.com"
mode = "websocket"
poll_interval = "12s"
event_buffer = 256
backoff.initial = "1s"
backoff.max = "30s"
backoff.max_retries = 10
`
}

func TestEnvVarOverrideNestedBackoff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(fullToml()), 0644))

	t.Setenv("ORACLE_CHAINS_ETHEREUM_BACKOFF_INITIAL", "5s")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.Chains["ethereum"].Backoff.Initial)
}

func TestEnvVarOverrideStaleness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(fullToml()), 0644))

	t.Setenv("ORACLE_CHAINLINK_STALENESS_THRESHOLD", "4h")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 4*time.Hour, cfg.Chainlink.StalenessThreshold)
}
