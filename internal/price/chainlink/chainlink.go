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

const (
	stalenessThreshold         = 2 * time.Hour
	abiWordSize                = 32
	latestRoundDataResponseLen = 160 // 5 ABI words
)

type RoundData struct {
	Answer    *big.Int
	UpdatedAt *big.Int
}

type ContractCaller interface {
	Decimals(ctx context.Context, feedAddr string) mo.Result[uint8]
	LatestRoundData(ctx context.Context, feedAddr string) mo.Result[RoundData]
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

	feed, ok := p.reg.LookupPriceFeed(p.chain, token).Get()
	if !ok {
		return mo.Err[float64](fmt.Errorf("no price feed for %s on %s", token, p.chain))
	}

	p.mu.RLock()
	if cached, ok := p.cache[feed.Address]; ok && time.Now().Before(cached.expiresAt) {
		p.mu.RUnlock()
		return mo.Ok(cached.price)
	}
	p.mu.RUnlock()

	decimals, err := p.getDecimals(ctx, feed.Address).Get()
	if err != nil {
		return mo.Err[float64](fmt.Errorf("decimals for %s: %w", token, err))
	}

	round, err := p.caller.LatestRoundData(ctx, feed.Address).Get()
	if err != nil {
		return mo.Err[float64](fmt.Errorf("latestRoundData for %s: %w", token, err))
	}

	updatedTime := time.Unix(round.UpdatedAt.Int64(), 0)
	if time.Since(updatedTime) > stalenessThreshold {
		logger.Warn("stale price feed",
			zap.String("token", token),
			zap.Time("updated_at", updatedTime),
		)
		return mo.Err[float64](fmt.Errorf("stale price for %s: updated %s ago", token, time.Since(updatedTime)))
	}

	price := new(big.Float).SetInt(round.Answer)
	divisor := new(big.Float).SetFloat64(math.Pow10(int(decimals)))
	price.Quo(price, divisor)
	priceF64, _ := price.Float64()

	p.mu.Lock()
	p.cache[feed.Address] = cachedPrice{price: priceF64, expiresAt: time.Now().Add(p.cfg.CacheTTL)}
	p.mu.Unlock()

	return mo.Ok(priceF64)
}

func (p *Provider) getDecimals(ctx context.Context, feedAddr string) mo.Result[uint8] {
	p.mu.RLock()
	if d, ok := p.decimals[feedAddr]; ok {
		p.mu.RUnlock()
		return mo.Ok(d)
	}
	p.mu.RUnlock()

	result := p.caller.Decimals(ctx, feedAddr)
	if d, err := result.Get(); err == nil {
		p.mu.Lock()
		p.decimals[feedAddr] = d
		p.mu.Unlock()
		return mo.Ok(d)
	}
	return result
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

func (c *EthCaller) Decimals(ctx context.Context, feedAddr string) mo.Result[uint8] {
	addr := common.HexToAddress(feedAddr)
	data := common.FromHex("0x313ce567") // decimals()
	msg := ethereum.CallMsg{To: &addr, Data: data}
	result, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return mo.Err[uint8](err)
	}
	if len(result) < abiWordSize {
		return mo.Err[uint8](fmt.Errorf("unexpected decimals response length: %d", len(result)))
	}
	return mo.Ok(uint8(new(big.Int).SetBytes(result).Uint64()))
}

func (c *EthCaller) LatestRoundData(ctx context.Context, feedAddr string) mo.Result[RoundData] {
	addr := common.HexToAddress(feedAddr)
	data := common.FromHex("0xfeaf968c") // latestRoundData()
	msg := ethereum.CallMsg{To: &addr, Data: data}
	result, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return mo.Err[RoundData](err)
	}
	if len(result) < latestRoundDataResponseLen {
		return mo.Err[RoundData](fmt.Errorf("unexpected latestRoundData response length: %d", len(result)))
	}
	return mo.Ok(RoundData{
		Answer:    new(big.Int).SetBytes(result[32:64]),
		UpdatedAt: new(big.Int).SetBytes(result[96:128]),
	})
}
