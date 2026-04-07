package evm

import (
	"sync"

	"github.com/samber/mo"
)

type BlockCache struct {
	mu      sync.RWMutex
	maxSize uint64
	entries map[uint64]int64
	latest  uint64
}

func NewBlockCache(maxSize int) *BlockCache {
	if maxSize < 1 {
		maxSize = 1
	}
	return &BlockCache{
		maxSize: uint64(maxSize),
		entries: make(map[uint64]int64),
	}
}

func (c *BlockCache) Get(block uint64) mo.Option[int64] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ts, ok := c.entries[block]; ok {
		return mo.Some(ts)
	}
	return mo.None[int64]()
}

func (c *BlockCache) Set(block uint64, timestamp int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[block] = timestamp
	if block > c.latest {
		c.latest = block
	}

	if c.latest > c.maxSize {
		cutoff := c.latest - c.maxSize
		for b := range c.entries {
			if b < cutoff {
				delete(c.entries, b)
			}
		}
	}
}
