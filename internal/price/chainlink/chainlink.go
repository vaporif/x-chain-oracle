package chainlink

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/samber/mo"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/config"
	"github.com/vaporif/x-chain-oracle/internal/registry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

const stalenessThreshold = 2 * time.Hour

type ContractCaller interface {
	Decimals(ctx context.Context, feedAddr string) (uint8, error)
	LatestRoundData(ctx context.Context, feedAddr string) (*big.Int, *big.Int, error)
}

type cachedPrice struct {
	price     float64
	expiresAt time.Time
}

type Provider struct {
	cfg      config.ChainlinkConfig
	caller   ContractCaller
	reg      *registry.Registry
	chain    types.ChainID
	mu       sync.RWMutex
	cache    map[string]cachedPrice
	decimals map[string]uint8
}

func NewWithCaller(cfg config.ChainlinkConfig, caller ContractCaller, reg *registry.Registry, chain types.ChainID) *Provider {
	return &Provider{
		cfg:      cfg,
		caller:   caller,
		reg:      reg,
		chain:    chain,
		cache:    make(map[string]cachedPrice),
		decimals: make(map[string]uint8),
	}
}

func (p *Provider) GetPriceUSD(ctx context.Context, token string) mo.Result[float64] {
	logger := zap.L().Named("chainlink")

	feed, ok := p.reg.LookupPriceFeed(p.chain, token)
	if !ok {
		return mo.Err[float64](fmt.Errorf("no price feed for %s on %s", token, p.chain))
	}

	p.mu.RLock()
	if cached, ok := p.cache[feed.Address]; ok && time.Now().Before(cached.expiresAt) {
		p.mu.RUnlock()
		return mo.Ok(cached.price)
	}
	p.mu.RUnlock()

	decimals, err := p.getDecimals(ctx, feed.Address)
	if err != nil {
		return mo.Err[float64](fmt.Errorf("decimals for %s: %w", token, err))
	}

	answer, updatedAt, err := p.caller.LatestRoundData(ctx, feed.Address)
	if err != nil {
		return mo.Err[float64](fmt.Errorf("latestRoundData for %s: %w", token, err))
	}

	updatedTime := time.Unix(updatedAt.Int64(), 0)
	if time.Since(updatedTime) > stalenessThreshold {
		logger.Warn("stale price feed",
			zap.String("token", token),
			zap.Time("updated_at", updatedTime),
		)
		return mo.Err[float64](fmt.Errorf("stale price for %s: updated %s ago", token, time.Since(updatedTime)))
	}

	price := new(big.Float).SetInt(answer)
	divisor := new(big.Float).SetFloat64(math.Pow10(int(decimals)))
	price.Quo(price, divisor)
	priceF64, _ := price.Float64()

	p.mu.Lock()
	p.cache[feed.Address] = cachedPrice{price: priceF64, expiresAt: time.Now().Add(p.cfg.CacheTTL)}
	p.mu.Unlock()

	return mo.Ok(priceF64)
}

func (p *Provider) getDecimals(ctx context.Context, feedAddr string) (uint8, error) {
	p.mu.RLock()
	if d, ok := p.decimals[feedAddr]; ok {
		p.mu.RUnlock()
		return d, nil
	}
	p.mu.RUnlock()

	d, err := p.caller.Decimals(ctx, feedAddr)
	if err != nil {
		return 0, err
	}

	p.mu.Lock()
	p.decimals[feedAddr] = d
	p.mu.Unlock()
	return d, nil
}

type EthCaller struct {
	client *ethclient.Client
}

func NewEthCaller(client *ethclient.Client) *EthCaller {
	return &EthCaller{client: client}
}

func New(cfg config.ChainlinkConfig, client *ethclient.Client, reg *registry.Registry, chain types.ChainID) *Provider {
	return NewWithCaller(cfg, NewEthCaller(client), reg, chain)
}

func (c *EthCaller) Decimals(ctx context.Context, feedAddr string) (uint8, error) {
	addr := common.HexToAddress(feedAddr)
	data := common.FromHex("0x313ce567") // decimals()
	msg := ethereum.CallMsg{To: &addr, Data: data}
	result, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return 0, err
	}
	if len(result) < 32 {
		return 0, fmt.Errorf("unexpected decimals response length: %d", len(result))
	}
	return uint8(new(big.Int).SetBytes(result).Uint64()), nil
}

func (c *EthCaller) LatestRoundData(ctx context.Context, feedAddr string) (*big.Int, *big.Int, error) {
	addr := common.HexToAddress(feedAddr)
	data := common.FromHex("0xfeaf968c") // latestRoundData()
	msg := ethereum.CallMsg{To: &addr, Data: data}
	result, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, nil, err
	}
	if len(result) < 160 { // 5 ABI words: roundId, answer, startedAt, updatedAt, answeredInRound
		return nil, nil, fmt.Errorf("unexpected latestRoundData response length: %d", len(result))
	}
	answer := new(big.Int).SetBytes(result[32:64])
	updatedAt := new(big.Int).SetBytes(result[96:128])
	return answer, updatedAt, nil
}
