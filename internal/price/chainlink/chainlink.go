package chainlink

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	"github.com/vaporif/x-chain-oracle/internal/config"
)

type Provider struct {
	cfg config.ChainlinkConfig
}

func New(cfg config.ChainlinkConfig) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) GetPriceUSD(_ context.Context, token string) mo.Result[float64] {
	return mo.Err[float64](fmt.Errorf("chainlink price feed not implemented for %s", token))
}
