package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/compiler/secret"
)

const observerEventCapacity = 256

const observerRecordCapacity = observerEventCapacity

var secretScrubberContextKey = xctx.NewKey[*secret.Scrubber]("flow.runner.secret-scrubber")

var errIntermediateActionFailed = errors.New("action failed; details omitted until final error sanitization")

func activeSecretScrubber(ctx context.Context, fallback *secret.Scrubber) *secret.Scrubber {
	if ctx != nil {
		if scrubber, ok := secretScrubberContextKey.From(ctx); ok && scrubber != nil {
			return scrubber
		}
	}
	return fallback
}

// SanitizeOutputText redacts captured secret values immediately before a
// capability emits final text to a user-visible or persistent destination.
func SanitizeOutputText(ctx context.Context, value string) string {
	if scrubber := activeSecretScrubber(ctx, nil); scrubber != nil {
		if sanitized, ok := scrubber.Sanitize(value).(string); ok {
			return sanitized
		}
		return secret.Redacted
	}
	return value
}

// Observer adapts Kernel lifecycle events to Flow's existing CLI trace while
// retaining a bounded, metadata-only snapshot for callers that need lifecycle
// information without action payloads.
type Observer struct {
	out       io.Writer
	verbosity int

	mu             sync.Mutex
	records        []Record
	actions        []string
	droppedRecords uint64
	droppedActions uint64
	events         *observe.MemorySink
}

type Record struct {
	Action   string
	Duration time.Duration
	Err      error
	Start    time.Time
}

func NewObserver(out io.Writer, verbosity int) *Observer {
	if out == nil {
		out = io.Discard
	}
	return &Observer{
		out:       out,
		verbosity: verbosity,
		events:    observe.NewMemorySink(observerEventCapacity),
	}
}

// Hook returns Kernel's standard lifecycle hook backed by this Flow adapter.
func (o *Observer) Hook() action.AnyHook {
	return observe.Hook(o)
}

// Emit stores a payload-free lifecycle snapshot and translates terminal action
// events into the legacy Flow records used by CLI summaries and assertions.
func (o *Observer) Emit(ctx context.Context, event observe.Event) {
	if o == nil {
		return
	}
	// Intermediate action payloads and error messages are never exposed by the
	// observer; errors are rendered only after Runner performs final sanitization.
	failed := event.Kind == observe.KindError
	if o.events != nil {
		o.events.Emit(ctx, metadataOnlyEvent(event))
	}
	switch event.Kind {
	case observe.KindSuccess:
		o.record(event, nil)
	case observe.KindError:
		if failed {
			o.record(event, errIntermediateActionFailed)
		}
	}
}

// Events returns an isolated, oldest-to-newest snapshot of the most recent
// Kernel lifecycle metadata. Request, response, error, and recovered values are
// intentionally omitted; callers that explicitly need raw events may provide
// Config.EventSink instead. The snapshot is bounded to observerEventCapacity.
func (o *Observer) Events() []observe.Event {
	if o == nil || o.events == nil {
		return nil
	}
	return o.events.Events()
}

func metadataOnlyEvent(event observe.Event) observe.Event {
	event.Request = nil
	event.Response = nil
	event.Error = nil
	event.Recovered = nil
	return event
}

func (o *Observer) record(event observe.Event, err error) {
	start := event.Time.Add(-event.Duration)
	if event.Time.IsZero() {
		start = time.Now().Add(-event.Duration)
	}
	dur := event.Duration

	o.mu.Lock()
	var dropped bool
	o.records, dropped = appendObserverHistory(o.records, Record{
		Action:   event.Action,
		Duration: dur,
		Err:      err,
		Start:    start,
	}, observerRecordCapacity)
	if dropped {
		o.droppedRecords++
	}
	o.actions, dropped = appendObserverHistory(o.actions, event.Action, observerRecordCapacity)
	if dropped {
		o.droppedActions++
	}
	o.mu.Unlock()

	// -vv prints live progress
	if o.verbosity == 2 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
	}
	// -vvv prints lifecycle progress without intermediate payloads.
	if o.verbosity >= 3 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
		fmt.Fprintln(o.out, "      in:  <omitted; intermediate payload>")
		if err != nil {
			fmt.Fprintln(o.out, "      err: <omitted; intermediate error>")
		} else {
			fmt.Fprintln(o.out, "      out: <omitted; intermediate payload>")
		}
	}
}

func (o *Observer) Records() []Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Record, len(o.records))
	copy(out, o.records)
	return out
}

func (o *Observer) ActionNames() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.actions...)
}

// ActionNamesComplete reports whether ActionNames contains the complete run history.
func (o *Observer) ActionNamesComplete() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.droppedActions == 0
}

func (o *Observer) PrintSummary(w io.Writer) {
	records := o.Records()
	if len(records) == 0 {
		return
	}
	o.mu.Lock()
	droppedRecords := o.droppedRecords
	o.mu.Unlock()

	fmt.Fprintln(w, "")
	if droppedRecords > 0 {
		fmt.Fprintf(w, "  (earlier trace records omitted; showing the most recent %d)\n", len(records))
	}
	fmt.Fprintln(w, "─── EXECUTION TRACE ───────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %-6s %-35s %10s   %s\n", "STATUS", "ACTION", "DURATION", "ERROR")

	var total time.Duration
	for _, r := range records {
		status := "✓"
		errStr := "-"
		if r.Err != nil {
			status = "✗"
			errStr = formatErrorSummary(r.Err)
			if len(errStr) > 40 {
				errStr = errStr[:37] + "..."
			}
		}
		fmt.Fprintf(w, "  %-6s %-35s %10s   %s\n", status, r.Action, FormatDuration(r.Duration), errStr)
		total += r.Duration
	}

	fmt.Fprintln(w, "───────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %-6s %-35s %10s\n", "", "TOTAL", FormatDuration(total))
	fmt.Fprintln(w, "")
}

func appendObserverHistory[T any](history []T, value T, capacity int) ([]T, bool) {
	if capacity <= 0 {
		return history, true
	}
	if len(history) < capacity {
		return append(history, value), false
	}
	copy(history, history[1:])
	history[len(history)-1] = value
	return history, true
}

// FormatDuration handles Windows clock resolution limits cleanly without misleading '0ns' outputs.
func FormatDuration(d time.Duration) string {
	if d <= 0 || d < time.Microsecond {
		return "< 1µs"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000.0)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

var _ observe.Sink = (*Observer)(nil)
