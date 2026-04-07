package pipeline

import (
	"context"
	"time"
)

type Traced[T any] struct {
	Value     T
	Ctx       context.Context
	StartedAt time.Time
}

func NewTraced[T any](ctx context.Context, value T) Traced[T] {
	return Traced[T]{Value: value, Ctx: ctx, StartedAt: time.Now()}
}
