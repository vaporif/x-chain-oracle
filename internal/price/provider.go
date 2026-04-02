package price

import (
	"context"

	"github.com/samber/mo"
)

type Provider interface {
	GetPriceUSD(ctx context.Context, token string) mo.Result[float64]
}
