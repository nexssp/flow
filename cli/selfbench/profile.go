package selfbench

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/nexssp/kernel/observe"
)

// ActionProfile aggregates lifecycle time across measured warm runs. Durations
// are inclusive, so nested actions may overlap and must not be summed as a total.
type ActionProfile struct {
	Action  string  `json:"action"`
	Calls   int     `json:"calls"`
	Errors  int     `json:"errors,omitempty"`
	TotalMs float64 `json:"total_ms"`
	MeanMs  float64 `json:"mean_ms"`
	MaxMs   float64 `json:"max_ms"`
}

type actionProfileStats struct {
	calls  int
	errors int
	total  time.Duration
	max    time.Duration
}

type actionProfiler struct {
	mu      sync.Mutex
	enabled bool
	stats   map[string]actionProfileStats
}

func newActionProfiler() *actionProfiler {
	return &actionProfiler{stats: make(map[string]actionProfileStats)}
}

func (p *actionProfiler) SetEnabled(enabled bool) {
	p.mu.Lock()
	if enabled {
		p.stats = make(map[string]actionProfileStats)
	}
	p.enabled = enabled
	p.mu.Unlock()
}

func (p *actionProfiler) Emit(_ context.Context, event observe.Event) {
	if p == nil || event.Action == "" || !profileTerminalEvent(event.Kind) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled {
		return
	}
	stats := p.stats[event.Action]
	stats.calls++
	stats.total += event.Duration
	if event.Duration > stats.max {
		stats.max = event.Duration
	}
	if event.Kind == observe.KindError || event.Kind == observe.KindCanceled || event.Kind == observe.KindPanic {
		stats.errors++
	}
	p.stats[event.Action] = stats
}

func profileTerminalEvent(kind string) bool {
	switch kind {
	case observe.KindSuccess, observe.KindError, observe.KindCacheHit,
		observe.KindCoalesced, observe.KindDeduplicated, observe.KindCanceled,
		observe.KindPanic:
		// Timeout and retry notifications are intermediate events followed by a
		// terminal success/error event; counting them would double-count calls.
		return true
	default:
		return false
	}
}

func (p *actionProfiler) Snapshot() []ActionProfile {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	profiles := make([]ActionProfile, 0, len(p.stats))
	for actionName, stats := range p.stats {
		if stats.calls == 0 {
			continue
		}
		totalMs := milliseconds(stats.total)
		profiles = append(profiles, ActionProfile{
			Action:  actionName,
			Calls:   stats.calls,
			Errors:  stats.errors,
			TotalMs: totalMs,
			MeanMs:  totalMs / float64(stats.calls),
			MaxMs:   milliseconds(stats.max),
		})
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].TotalMs == profiles[j].TotalMs {
			return profiles[i].Action < profiles[j].Action
		}
		return profiles[i].TotalMs > profiles[j].TotalMs
	})
	return profiles
}

func combineEventSinks(existing, additional observe.Sink) observe.Sink {
	if existing == nil {
		return additional
	}
	if additional == nil {
		return existing
	}
	return eventSinkGroup{existing, additional}
}

type eventSinkGroup []observe.Sink

func (s eventSinkGroup) Emit(ctx context.Context, event observe.Event) {
	for _, sink := range s {
		if sink != nil {
			sink.Emit(ctx, event)
		}
	}
}

var _ observe.Sink = (*actionProfiler)(nil)
