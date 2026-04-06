package evm

import "sync"

type BlockCache struct {
	mu      sync.RWMutex
	entries map[uint64]int64
	latest  uint64
}

func NewBlockCache() *BlockCache {
	return &BlockCache{entries: make(map[uint64]int64)}
}

func (c *BlockCache) Get(block uint64) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ts, ok := c.entries[block]
	return ts, ok
}

func (c *BlockCache) Set(block uint64, timestamp int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[block] = timestamp
	if block > c.latest {
		c.latest = block
	}

	if c.latest > 100 {
		cutoff := c.latest - 100
		for b := range c.entries {
			if b < cutoff {
				delete(c.entries, b)
			}
		}
	}
}
