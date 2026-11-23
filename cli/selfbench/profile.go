package selfbench

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

var actionProfileSpanKey = xctx.NewKey[*actionSpan]("flow.selfbench.action-profile-span")

// ActionProfile aggregates one node in an action call tree across measured
// runs. TotalMs is inclusive wall time; SelfMs subtracts the union of direct
// child spans and is therefore exclusive wall time, not CPU time.
type ActionProfile struct {
	Action     string  `json:"action"`
	Path       string  `json:"path"`
	Parent     string  `json:"parent,omitempty"`
	Depth      int     `json:"depth"`
	Calls      int     `json:"calls"`
	Errors     int     `json:"errors,omitempty"`
	Retries    int     `json:"retries,omitempty"`
	TotalMs    float64 `json:"total_ms"`
	SelfMs     float64 `json:"self_ms"`
	MeanMs     float64 `json:"mean_ms"`
	MeanSelfMs float64 `json:"mean_self_ms"`
	MaxMs      float64 `json:"max_ms"`
	MaxSelfMs  float64 `json:"max_self_ms"`
}

type actionSpan struct {
	action  string
	path    string
	parent  *actionSpan
	depth   int
	started time.Time
	ended   time.Time
	err     bool
	retries int
}

// ActionProfiler records action invocations while enabled. Hooks are attached
// to resolved actions and streams by runner.Config.Hooks, so nested executions
// inherit parent spans through their context.
type ActionProfiler struct {
	mu      sync.Mutex
	enabled bool
	spans   []*actionSpan
}

// NewActionProfiler constructs a profiler ready to be attached to a runner.
func NewActionProfiler() *ActionProfiler { return &ActionProfiler{} }

// Hook returns the Kernel hook that records action start, completion, errors,
// and retries. Install it in runner.Config.Hooks before compiling the workflow.
func (p *ActionProfiler) Hook() action.AnyHook {
	if p == nil {
		return action.AnyHook{}
	}
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, meta *action.Meta) (context.Context, error) {
			p.mu.Lock()
			if !p.enabled {
				p.mu.Unlock()
				return ctx, nil
			}
			parent, _ := actionProfileSpanKey.From(ctx)
			name := ""
			if meta != nil {
				name = meta.Name
			}
			span := &actionSpan{action: name, parent: parent, started: time.Now()}
			if parent == nil {
				span.path = name
			} else {
				span.path = parent.path + " > " + name
				span.depth = parent.depth + 1
			}
			p.spans = append(p.spans, span)
			p.mu.Unlock()
			return actionProfileSpanKey.With(ctx, span), nil
		},
		After: func(ctx context.Context, _, _ any, err error, _ *action.Meta) {
			span, ok := actionProfileSpanKey.From(ctx)
			if !ok || span == nil {
				return
			}
			p.mu.Lock()
			span.ended = time.Now()
			span.err = err != nil
			p.mu.Unlock()
		},
		OnRetry: func(ctx context.Context, _ any, _ int, _ error, _ *action.Meta) {
			span, ok := actionProfileSpanKey.From(ctx)
			if !ok || span == nil {
				return
			}
			p.mu.Lock()
			span.retries++
			p.mu.Unlock()
		},
	}
}

// Start clears prior measurements and begins recording. It is safe to reuse a
// profiler for separate measured windows after the previous window has stopped.
func (p *ActionProfiler) Start() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.spans = p.spans[:0]
	p.enabled = true
	p.mu.Unlock()
}

// Stop ends the current measurement window. Already-started spans still finish
// normally and remain in the snapshot.
func (p *ActionProfiler) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.enabled = false
	p.mu.Unlock()
}

// Snapshot returns action call-tree aggregates sorted by path. Child intervals
// are merged before subtraction, so concurrent overlapping siblings are never
// subtracted twice from their parent's exclusive wall time.
func (p *ActionProfiler) Snapshot() []ActionProfile {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	children := make(map[*actionSpan][]timeInterval, len(p.spans))
	for _, span := range p.spans {
		if span.parent != nil && !span.ended.IsZero() {
			children[span.parent] = append(children[span.parent], timeInterval{start: span.started, end: span.ended})
		}
	}

	type aggregate struct {
		profile ActionProfile
		maxSelf time.Duration
	}
	aggregates := make(map[string]*aggregate, len(p.spans))
	for _, span := range p.spans {
		stats := aggregates[span.path]
		if stats == nil {
			stats = &aggregate{profile: ActionProfile{
				Action: span.action, Path: span.path, Depth: span.depth,
			}}
			if span.parent != nil {
				stats.profile.Parent = span.parent.path
			}
			aggregates[span.path] = stats
		}
		stats.profile.Calls++
		if span.err {
			stats.profile.Errors++
		}
		stats.profile.Retries += span.retries
		inclusive := spanDuration(span.started, span.ended)
		self := exclusiveWallTime(span.started, span.ended, children[span])
		stats.profile.TotalMs += milliseconds(inclusive)
		stats.profile.SelfMs += milliseconds(self)
		if milliseconds(inclusive) > stats.profile.MaxMs {
			stats.profile.MaxMs = milliseconds(inclusive)
		}
		if self > stats.maxSelf {
			stats.maxSelf = self
		}
	}

	profiles := make([]ActionProfile, 0, len(aggregates))
	for _, stats := range aggregates {
		if stats.profile.Calls == 0 {
			continue
		}
		stats.profile.MeanMs = stats.profile.TotalMs / float64(stats.profile.Calls)
		stats.profile.MeanSelfMs = stats.profile.SelfMs / float64(stats.profile.Calls)
		stats.profile.MaxSelfMs = milliseconds(stats.maxSelf)
		profiles = append(profiles, stats.profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Path < profiles[j].Path })
	return profiles
}

type timeInterval struct {
	start time.Time
	end   time.Time
}

func spanDuration(start, end time.Time) time.Duration {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

func exclusiveWallTime(start, end time.Time, children []timeInterval) time.Duration {
	inclusive := spanDuration(start, end)
	if inclusive == 0 || len(children) == 0 {
		return inclusive
	}
	intervals := make([]timeInterval, 0, len(children))
	for _, child := range children {
		if child.start.Before(start) {
			child.start = start
		}
		if child.end.After(end) {
			child.end = end
		}
		if child.end.After(child.start) {
			intervals = append(intervals, child)
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })

	covered := time.Duration(0)
	cursor := start
	for _, child := range intervals {
		if !child.end.After(cursor) {
			continue
		}
		if child.start.After(cursor) {
			cursor = child.start
		}
		covered += child.end.Sub(cursor)
		cursor = child.end
	}
	if covered >= inclusive {
		return 0
	}
	return inclusive - covered
}
