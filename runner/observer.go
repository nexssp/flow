package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"

	"github.com/nexssp/flow/compiler/redact"
)

const observerEventCapacity = 256

// Observer adapts Kernel lifecycle events to Flow's existing CLI trace while
// retaining a bounded, metadata-only snapshot for callers that need lifecycle
// information without action payloads.
type Observer struct {
	out       io.Writer
	verbosity int

	mu      sync.Mutex
	records []Record
	events  *observe.MemorySink
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
	if o.events != nil {
		o.events.Emit(ctx, metadataOnlyEvent(event))
	}
	switch event.Kind {
	case observe.KindSuccess:
		o.record(event, nil)
	case observe.KindError:
		o.record(event, event.Error)
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
	o.records = append(o.records, Record{
		Action:   event.Action,
		Duration: dur,
		Err:      err,
		Start:    start,
	})
	o.mu.Unlock()

	// -vv prints live progress
	if o.verbosity == 2 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
	}
	// -vvv prints live progress WITH data payloads
	if o.verbosity >= 3 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
		fmt.Fprintf(o.out, "      in:  %s\n", formatTracePayload(event.Request))
		if err != nil {
			fmt.Fprintf(o.out, "      err: %v\n", err)
		} else {
			fmt.Fprintf(o.out, "      out: %s\n", formatTracePayload(event.Response))
		}
	}
}

func formatTracePayload(v any) string {
	if v == nil {
		return "nil"
	}
	if m, ok := v.(map[string]any); ok {
		v = redact.Map(m)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := string(b)
	if len(s) > 256 {
		return s[:253] + "..."
	}
	return s
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
	out := make([]string, len(o.records))
	for i, r := range o.records {
		out[i] = r.Action
	}
	return out
}

func (o *Observer) PrintSummary(w io.Writer) {
	records := o.Records()
	if len(records) == 0 {
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "─── EXECUTION TRACE ───────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %-6s %-35s %10s   %s\n", "STATUS", "ACTION", "DURATION", "ERROR")

	var total time.Duration
	for _, r := range records {
		status := "✓"
		errStr := "-"
		if r.Err != nil {
			status = "✗"
			errStr = r.Err.Error()
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
