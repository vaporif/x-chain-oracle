package adapter

import (
	"context"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type ChainAdapter interface {
	Start(ctx context.Context) error
	Events() <-chan types.RawEvent
	Chain() types.ChainID
}
