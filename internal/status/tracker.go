package status

import (
	"sync"
	"time"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type ChainState struct {
	Connected   bool
	LastBlock   uint64
	LastEventAt int64
}

type Tracker struct {
	mu        sync.RWMutex
	chains    map[types.ChainID]*ChainState
	startedAt time.Time
}

func NewTracker() *Tracker {
	return &Tracker{
		chains:    make(map[types.ChainID]*ChainState),
		startedAt: time.Now(),
	}
}

func (t *Tracker) SetConnected(chain types.ChainID, connected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.getOrCreate(chain)
	state.Connected = connected
}

func (t *Tracker) UpdateBlock(chain types.ChainID, block uint64, timestamp int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.getOrCreate(chain)
	state.LastBlock = block
	state.LastEventAt = timestamp
}

func (t *Tracker) Snapshot() (map[types.ChainID]*ChainState, time.Duration) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cp := make(map[types.ChainID]*ChainState, len(t.chains))
	for k, v := range t.chains {
		clone := *v
		cp[k] = &clone
	}
	return cp, time.Since(t.startedAt)
}

func (t *Tracker) getOrCreate(chain types.ChainID) *ChainState {
	if s, ok := t.chains[chain]; ok {
		return s
	}
	s := &ChainState{}
	t.chains[chain] = s
	return s
}
