package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	LogLevel  string                 `koanf:"log_level"`
	GRPC      GRPCConfig             `koanf:"grpc"`
	Enricher  EnricherConfig         `koanf:"enricher"`
	Chainlink ChainlinkConfig        `koanf:"chainlink"`
	Engine    EngineConfig           `koanf:"engine"`
	Pipeline  PipelineConfig         `koanf:"pipeline"`
	Chains    map[string]ChainConfig `koanf:"chains"`
}

type PipelineConfig struct {
	RawEventBuffer      int `koanf:"raw_event_buffer"`
	ChainEventBuffer    int `koanf:"chain_event_buffer"`
	EnrichedEventBuffer int `koanf:"enriched_event_buffer"`
	SignalBuffer        int `koanf:"signal_buffer"`
}

type GRPCConfig struct {
	Port                 int `koanf:"port"`
	SubscriberBufferSize int `koanf:"subscriber_buffer_size"`
}

type EnricherConfig struct {
	Workers int `koanf:"workers"`
}

type ChainlinkConfig struct {
	CacheTTL           time.Duration `koanf:"cache_ttl"`
	StalenessThreshold time.Duration `koanf:"staleness_threshold"`
}

type ChainConfig struct {
	RPCURL       string        `koanf:"rpc_url"`
	Mode         string        `koanf:"mode"`
	PollInterval time.Duration `koanf:"poll_interval"`
	EventBuffer  int           `koanf:"event_buffer"`
	Backoff      BackoffConfig `koanf:"backoff"`
}

type BackoffConfig struct {
	Initial    time.Duration `koanf:"initial"`
	Max        time.Duration `koanf:"max"`
	MaxRetries int           `koanf:"max_retries"`
}

type EngineConfig struct {
	DefaultWindowTTL time.Duration `koanf:"default_window_ttl"`
	PruneInterval    time.Duration `koanf:"prune_interval"`
	MaxWindowSize    int           `koanf:"max_window_size"`
}

func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}

	existingKeys := k.Keys()
	if err := k.Load(env.Provider("ORACLE_", ".", func(s string) string {
		key := strings.ToLower(strings.TrimPrefix(s, "ORACLE_"))
		return bestKeyMatch(key, existingKeys)
	}), nil); err != nil {
		return nil, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.GRPC.SubscriberBufferSize == 0 {
		cfg.GRPC.SubscriberBufferSize = 64
	}
	if cfg.Chainlink.StalenessThreshold == 0 {
		cfg.Chainlink.StalenessThreshold = 2 * time.Hour
	}
	if cfg.Engine.DefaultWindowTTL == 0 {
		cfg.Engine.DefaultWindowTTL = 30 * time.Second
	}
	if cfg.Engine.PruneInterval == 0 {
		cfg.Engine.PruneInterval = 5 * time.Second
	}
	if cfg.Engine.MaxWindowSize == 0 {
		cfg.Engine.MaxWindowSize = 10000
	}
	if cfg.Pipeline.RawEventBuffer == 0 {
		cfg.Pipeline.RawEventBuffer = 512
	}
	if cfg.Pipeline.ChainEventBuffer == 0 {
		cfg.Pipeline.ChainEventBuffer = 256
	}
	if cfg.Pipeline.EnrichedEventBuffer == 0 {
		cfg.Pipeline.EnrichedEventBuffer = 64
	}
	if cfg.Pipeline.SignalBuffer == 0 {
		cfg.Pipeline.SignalBuffer = 32
	}
	for name, chain := range cfg.Chains {
		if chain.PollInterval == 0 {
			chain.PollInterval = 12 * time.Second
		}
		if chain.EventBuffer == 0 {
			chain.EventBuffer = 256
		}
		if chain.Backoff.Initial == 0 {
			chain.Backoff.Initial = 1 * time.Second
		}
		if chain.Backoff.Max == 0 {
			chain.Backoff.Max = 30 * time.Second
		}
		if chain.Backoff.MaxRetries == 0 {
			chain.Backoff.MaxRetries = 10
		}
		cfg.Chains[name] = chain
	}
}

func (c *Config) Validate() error {
	if c.GRPC.Port < 1 || c.GRPC.Port > 65535 {
		return fmt.Errorf("grpc.port must be 1-65535, got %d", c.GRPC.Port)
	}
	if c.Enricher.Workers < 1 {
		return fmt.Errorf("enricher.workers must be >= 1, got %d", c.Enricher.Workers)
	}
	if c.Chainlink.CacheTTL <= 0 {
		return fmt.Errorf("chainlink.cache_ttl must be > 0")
	}
	if c.Chainlink.StalenessThreshold <= 0 {
		return fmt.Errorf("chainlink.staleness_threshold must be > 0")
	}
	if c.Engine.MaxWindowSize < 1 {
		return fmt.Errorf("engine.max_window_size must be >= 1, got %d", c.Engine.MaxWindowSize)
	}
	if c.Engine.PruneInterval <= 0 {
		return fmt.Errorf("engine.prune_interval must be > 0")
	}
	for name, chain := range c.Chains {
		if chain.RPCURL == "" {
			return fmt.Errorf("chains.%s.rpc_url must not be empty", name)
		}
		if chain.PollInterval <= 0 {
			return fmt.Errorf("chains.%s.poll_interval must be > 0", name)
		}
	}
	return nil
}

// ORACLE_CHAINS_ETHEREUM_RPC_URL could be chains.ethereum.rpc.url or chains.ethereum.rpc_url —
// try all dot/underscore combos and pick the one that matches an existing key.
func bestKeyMatch(envKey string, existingKeys []string) string {
	existing := make(map[string]struct{}, len(existingKeys))
	for _, k := range existingKeys {
		existing[k] = struct{}{}
	}

	parts := strings.Split(envKey, "_")
	if len(parts) == 1 {
		return envKey
	}

	var best string
	var search func(idx int, current string)
	search = func(idx int, current string) {
		if idx == len(parts) {
			if _, ok := existing[current]; ok {
				best = current
			}
			return
		}
		if current == "" {
			search(idx+1, parts[idx])
			return
		}
		search(idx+1, current+"_"+parts[idx])
		search(idx+1, current+"."+parts[idx])
	}
	search(0, "")

	if best != "" {
		return best
	}
	return strings.ReplaceAll(envKey, "_", ".")
}
