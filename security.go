package flow

import (
	"context"
	"time"

	"github.com/nexssp/kernel/action"
)

// SecurityEvent is one observation of a profile hook firing around a
// node. Every profile hook (guard.prompt_injection, guard.pii_redact,
// guard.network_ssrf, …) emits one "before" and one "after" event per
// node it wraps. A guard that rejects produces "before" with Err set.
//
// The event is deliberately small and flat: it is meant to be rendered
// as a single log line, stored as a JSONL row, or counted as a metric
// without further processing.
type SecurityEvent struct {
	Hook    string        // "guard.prompt_injection"
	Node    string        // "agent.architect"
	Phase   string        // "before" | "after"
	Elapsed time.Duration // duration of the hook call itself
	Err     error         // non-nil when the hook rejected the request
}

// SecurityObserver receives SecurityEvents from the compiler. The
// interface is a single method so any consumer — a runner observer, a
// test double, an OpenTelemetry bridge — can implement it without
// touching the flow package.
type SecurityObserver interface {
	OnSecurity(ctx context.Context, ev SecurityEvent)
}

// wrapProfileHook returns a copy of h whose Before, After, and OnError
// callbacks are wrapped so that firing the hook also emits a
// SecurityEvent. The wrapped hook has the same semantics as the
// original: if h.Before returns an error, the wrapper propagates it
// unchanged, and the emitted event carries the same error.
//
// When observer is nil, wrapProfileHook returns h unchanged, so the
// security instrumentation costs nothing when no one is watching.
func wrapProfileHook(h action.AnyHook, hookName string, observer SecurityObserver) action.AnyHook {
	if observer == nil {
		return h
	}

	wrapped := action.AnyHook{
		OnBuild: h.OnBuild,
	}

	if h.Before != nil {
		original := h.Before
		wrapped.Before = func(ctx context.Context, req any, meta *action.Meta) (context.Context, error) {
			start := time.Now()
			newCtx, err := original(ctx, req, meta)

			observer.OnSecurity(ctx, SecurityEvent{
				Hook:    hookName,
				Node:    metaName(meta),
				Phase:   "before",
				Elapsed: time.Since(start),
				Err:     err,
			})

			return newCtx, err
		}
	}

	if h.After != nil {
		original := h.After
		wrapped.After = func(ctx context.Context, req, res any, err error, meta *action.Meta) {
			start := time.Now()
			original(ctx, req, res, err, meta)

			observer.OnSecurity(ctx, SecurityEvent{
				Hook:    hookName,
				Node:    metaName(meta),
				Phase:   "after",
				Elapsed: time.Since(start),
				Err:     err,
			})
		}
	}

	if h.OnError != nil {
		original := h.OnError
		wrapped.OnError = func(ctx context.Context, req any, err error, meta *action.Meta) {
			original(ctx, req, err, meta)

			observer.OnSecurity(ctx, SecurityEvent{
				Hook:  hookName,
				Node:  metaName(meta),
				Phase: "error",
				Err:   err,
			})
		}
	}

	return wrapped
}

func metaName(m *action.Meta) string {
	if m == nil {
		return ""
	}

	return m.Name
}
