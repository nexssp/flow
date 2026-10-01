package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
)

// Observer tracks every action that passes through the runtime.
type Observer struct {
	out       io.Writer
	verbosity int

	mu      sync.Mutex
	records []Record
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
	return &Observer{out: out, verbosity: verbosity}
}

type startKey struct{}

func (o *Observer) Hook() action.AnyHook {
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			return context.WithValue(ctx, startKey{}, time.Now()), nil
		},
		OnSuccess: func(ctx context.Context, req, res any, meta *action.Meta) {
			o.record(ctx, meta, req, res, nil)
		},
		OnError: func(ctx context.Context, req any, err error, meta *action.Meta) {
			o.record(ctx, meta, req, nil, err)
		},
	}
}

func (o *Observer) record(ctx context.Context, meta *action.Meta, req, res any, err error) {
	start, _ := ctx.Value(startKey{}).(time.Time)
	dur := time.Since(start)

	o.mu.Lock()
	o.records = append(o.records, Record{
		Action:   meta.Name,
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
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, meta.Name, FormatDuration(dur))
	}
	// -vvv prints live progress WITH data payloads
	if o.verbosity >= 3 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, meta.Name, FormatDuration(dur))
		fmt.Fprintf(o.out, "      in:  %s\n", formatTracePayload(req))
		if err != nil {
			fmt.Fprintf(o.out, "      err: %v\n", err)
		} else {
			fmt.Fprintf(o.out, "      out: %s\n", formatTracePayload(res))
		}
	}
}

func formatTracePayload(v any) string {
	if v == nil {
		return "nil"
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
