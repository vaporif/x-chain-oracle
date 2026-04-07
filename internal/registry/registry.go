package registry

import (
	"maps"
	"slices"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/mo"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type ContractInfo struct {
	Name                   string `koanf:"name"`
	Protocol               string `koanf:"protocol"`
	MedianBridgeLatency    mo.Option[time.Duration]
	RawMedianBridgeLatency string `koanf:"median_bridge_latency"`
}

type PriceFeed struct {
	Address string `koanf:"address"`
}

type WormholeAddresses struct {
	Core        string `koanf:"core"`
	TokenBridge string `koanf:"token_bridge"`
}

type Registry struct {
	contracts  map[types.ChainID]map[string]ContractInfo
	priceFeeds map[types.ChainID]map[string]PriceFeed
	wormhole   map[types.ChainID]WormholeAddresses
}

func (r *Registry) LookupContract(chain types.ChainID, address string) mo.Option[ContractInfo] {
	if chainContracts, ok := r.contracts[chain]; ok {
		if info, ok := chainContracts[address]; ok {
			return mo.Some(info)
		}
	}
	return mo.None[ContractInfo]()
}

func (r *Registry) LookupPriceFeed(chain types.ChainID, token string) mo.Option[PriceFeed] {
	if chainFeeds, ok := r.priceFeeds[chain]; ok {
		if feed, ok := chainFeeds[token]; ok {
			return mo.Some(feed)
		}
	}
	return mo.None[PriceFeed]()
}

func (r *Registry) ContractAddresses(chain types.ChainID) []string {
	chainContracts, ok := r.contracts[chain]
	if !ok {
		return nil
	}
	return slices.Collect(maps.Keys(chainContracts))
}

func (r *Registry) WormholeConfig(chain types.ChainID) (WormholeAddresses, bool) {
	wh, ok := r.wormhole[chain]
	return wh, ok
}

type rawRegistry struct {
	Contracts  map[string]map[string]ContractInfo `koanf:"contracts"`
	PriceFeeds map[string]map[string]PriceFeed    `koanf:"price_feeds"`
	Wormhole   map[string]WormholeAddresses       `koanf:"wormhole"`
}

func Load(path string) (*Registry, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}

	var raw rawRegistry
	if err := k.Unmarshal("", &raw); err != nil {
		return nil, err
	}

	reg := &Registry{
		contracts:  make(map[types.ChainID]map[string]ContractInfo),
		priceFeeds: make(map[types.ChainID]map[string]PriceFeed),
		wormhole:   make(map[types.ChainID]WormholeAddresses),
	}

	for chain, contracts := range raw.Contracts {
		cid := types.ChainID(chain)
		reg.contracts[cid] = make(map[string]ContractInfo)
		for addr, info := range contracts {
			if info.RawMedianBridgeLatency != "" {
				if d, err := time.ParseDuration(info.RawMedianBridgeLatency); err == nil {
					info.MedianBridgeLatency = mo.Some(d)
				} else {
					zap.L().Named("registry").Warn("invalid median_bridge_latency, ignoring",
						zap.String("chain", chain),
						zap.String("address", addr),
						zap.String("value", info.RawMedianBridgeLatency),
						zap.Error(err),
					)
				}
			}
			reg.contracts[cid][addr] = info
		}
	}

	for chain, feeds := range raw.PriceFeeds {
		cid := types.ChainID(chain)
		reg.priceFeeds[cid] = make(map[string]PriceFeed)
		for token, feed := range feeds {
			reg.priceFeeds[cid][token] = feed
		}
	}

	for chain, wh := range raw.Wormhole {
		reg.wormhole[types.ChainID(chain)] = wh
	}

	return reg, nil
}
