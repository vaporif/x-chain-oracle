package evm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
)

func TestBlockCacheGetSet(t *testing.T) {
	cache := evm.NewBlockCache()
	cache.Set(100, 1700000000)

	ts, ok := cache.Get(100)
	assert.True(t, ok)
	assert.Equal(t, int64(1700000000), ts)
}

func TestBlockCacheMiss(t *testing.T) {
	cache := evm.NewBlockCache()
	_, ok := cache.Get(999)
	assert.False(t, ok)
}

func TestBlockCacheEviction(t *testing.T) {
	cache := evm.NewBlockCache()
	cache.Set(100, 1700000000)
	cache.Set(201, 1700001200) // 101 blocks later

	_, ok := cache.Get(100)
	assert.False(t, ok, "block 100 should be evicted (>100 blocks behind 201)")

	ts, ok := cache.Get(201)
	assert.True(t, ok)
	assert.Equal(t, int64(1700001200), ts)
}

func TestBlockCacheNoEvictionWithin100(t *testing.T) {
	cache := evm.NewBlockCache()
	cache.Set(100, 1700000000)
	cache.Set(200, 1700001200) // exactly 100 blocks later

	_, ok := cache.Get(100)
	assert.True(t, ok, "block 100 should NOT be evicted (exactly 100 blocks behind)")
}
