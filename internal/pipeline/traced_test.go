package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/pipeline"
)

func TestNewTraced(t *testing.T) {
	ctx := context.Background()
	before := time.Now()
	traced := pipeline.NewTraced(ctx, "hello")
	after := time.Now()

	assert.Equal(t, "hello", traced.Value)
	assert.Equal(t, ctx, traced.Ctx)
	assert.False(t, traced.StartedAt.Before(before))
	assert.False(t, traced.StartedAt.After(after))
}

func TestTracedThroughChannel(t *testing.T) {
	ch := make(chan pipeline.Traced[int], 1)
	ctx := context.Background()
	ch <- pipeline.NewTraced(ctx, 42)
	got := <-ch
	assert.Equal(t, 42, got.Value)
	assert.NotNil(t, got.Ctx)
}
