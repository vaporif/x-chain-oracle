package status_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/status"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func TestTrackerSetConnected(t *testing.T) {
	tr := status.NewTracker()

	tr.SetConnected(types.ChainEthereum, true)
	snap, _ := tr.Snapshot()

	state, ok := snap[types.ChainEthereum]
	assert.True(t, ok)
	assert.True(t, state.Connected)
}

func TestTrackerUpdateBlock(t *testing.T) {
	tr := status.NewTracker()

	tr.UpdateBlock(types.ChainEthereum, 100, 1700000000)
	snap, _ := tr.Snapshot()

	state := snap[types.ChainEthereum]
	assert.Equal(t, uint64(100), state.LastBlock)
	assert.Equal(t, int64(1700000000), state.LastEventAt)
}

func TestTrackerUptime(t *testing.T) {
	tr := status.NewTracker()
	_, uptime := tr.Snapshot()
	assert.Greater(t, uptime.Seconds(), float64(0))
}

func TestTrackerSnapshotIsCopy(t *testing.T) {
	tr := status.NewTracker()
	tr.SetConnected(types.ChainEthereum, true)

	snap, _ := tr.Snapshot()
	snap[types.ChainEthereum].Connected = false

	snap2, _ := tr.Snapshot()
	assert.True(t, snap2[types.ChainEthereum].Connected, "mutation of snapshot should not affect tracker")
}

func TestTrackerDisconnect(t *testing.T) {
	tr := status.NewTracker()
	tr.SetConnected(types.ChainEthereum, true)
	tr.SetConnected(types.ChainEthereum, false)

	snap, _ := tr.Snapshot()
	assert.False(t, snap[types.ChainEthereum].Connected)
}
