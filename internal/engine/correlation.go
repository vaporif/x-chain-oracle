package engine

import (
	"sync"
	"time"

	"slices"

	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type CorrelatorConfig struct {
	DefaultWindowTTL time.Duration
	PruneInterval    time.Duration
	MaxWindowSize    int
}

type windowEntry struct {
	event     types.EnrichedEvent
	expiresAt time.Time
}

type EventWindow struct {
	mu      sync.Mutex
	entries []windowEntry
	maxSize int
}

func (w *EventWindow) Add(event types.EnrichedEvent, ttl time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, windowEntry{
		event:     event,
		expiresAt: time.Now().Add(ttl),
	})
	if w.maxSize > 0 && len(w.entries) > w.maxSize {
		drop := len(w.entries) - w.maxSize
		zap.L().Named("engine.correlation").Warn("window size cap reached, dropping oldest entries",
			zap.Int("dropped", drop),
			zap.Int("max_size", w.maxSize),
		)
		w.entries = w.entries[drop:]
	}
}

func (w *EventWindow) MatchingEntries(sameFields []string, event types.EnrichedEvent) []types.EnrichedEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	var matches []types.EnrichedEvent
	for _, entry := range w.entries {
		if entry.expiresAt.Before(now) {
			continue
		}
		if fieldsMatch(sameFields, entry.event, event) {
			matches = append(matches, entry.event)
		}
	}
	return matches
}

func (w *EventWindow) Prune() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	w.entries = slices.DeleteFunc(w.entries, func(e windowEntry) bool {
		return !e.expiresAt.After(now)
	})
}

func fieldsMatch(fields []string, a, b types.EnrichedEvent) bool {
	for _, f := range fields {
		if getFieldValue(f, a) != getFieldValue(f, b) {
			return false
		}
	}
	return true
}

type Correlator struct {
	correlations []Correlation
	windows      map[string]*EventWindow
	windowTTLs   map[string]time.Duration
	cfg          CorrelatorConfig
}

func NewCorrelator(correlations []Correlation, cfg CorrelatorConfig) *Correlator {
	c := &Correlator{
		windows:    make(map[string]*EventWindow),
		windowTTLs: make(map[string]time.Duration),
		cfg:        cfg,
	}
	for i, corr := range correlations {
		d, err := time.ParseDuration(corr.Window)
		if err != nil {
			d = cfg.DefaultWindowTTL
		}
		correlations[i].windowDuration = d
		for _, evtType := range corr.Sequence {
			if _, ok := c.windows[evtType]; !ok {
				c.windows[evtType] = &EventWindow{maxSize: cfg.MaxWindowSize}
			}
			if d > c.windowTTLs[evtType] {
				c.windowTTLs[evtType] = d
			}
		}
	}
	c.correlations = correlations
	return c
}

func (c *Correlator) StartPruner(done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(c.cfg.PruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				for _, w := range c.windows {
					w.Prune()
				}
			case <-done:
				return
			}
		}
	}()
}

func (c *Correlator) Process(event types.EnrichedEvent) []types.Signal {
	evtType := string(event.EventType)
	var signals []types.Signal

	for _, corr := range c.correlations {
		if len(corr.Sequence) != 2 {
			continue
		}

		if corr.Sequence[1] == evtType {
			firstType := corr.Sequence[0]
			window, ok := c.windows[firstType]
			if !ok {
				continue
			}

			matches := window.MatchingEntries(corr.SameFields, event)
			minCount := corr.MinFirstCount
			if minCount <= 0 {
				minCount = 1
			}
			if len(matches) >= minCount {
				sig := newSignal(corr.Signal, corr.Confidence, event, map[string]string{"correlation": corr.Name})
				signals = append(signals, sig)
			}
		}
	}

	if window, ok := c.windows[evtType]; ok {
		ttl := c.windowTTLs[evtType]
		if ttl == 0 {
			ttl = c.cfg.DefaultWindowTTL
		}
		window.Add(event, ttl)
	}

	return signals
}
