package config

import (
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	GRPC      GRPCConfig             `koanf:"grpc"`
	Enricher  EnricherConfig         `koanf:"enricher"`
	Chainlink ChainlinkConfig        `koanf:"chainlink"`
	Chains    map[string]ChainConfig `koanf:"chains"`
}

type GRPCConfig struct {
	Port int `koanf:"port"`
}

type EnricherConfig struct {
	Workers int `koanf:"workers"`
}

type ChainlinkConfig struct {
	CacheTTL time.Duration `koanf:"cache_ttl"`
}

type ChainConfig struct {
	RPCURL string `koanf:"rpc_url"`
	Mode   string `koanf:"mode"`
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
	return &cfg, nil
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
