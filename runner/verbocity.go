package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Verbosity levels. Each level adds a NEW CATEGORY of information,
// not just "more of the same".
//
//	Silent    — no diagnostics, only final output
//	Summary   — one-line summary at the end (-v)
//	Timing    — per-node timing and counters (-vv)
//	Lifecycle — config merge, hook firing, state transitions (-vvv)
//	Data      — payload inspection per atom, truncated (-vvvv)
//
// Levels 0-3 are implemented in v2.0. Level 4 is specified now to
// avoid a breaking change later, implemented in F2.
const (
	VerbositySilent    = 0
	VerbositySummary   = 1
	VerbosityTiming    = 2
	VerbosityLifecycle = 3
	VerbosityData      = 4
)

// LogLifecycle emits a lifecycle-level trace line. Format is intentionally
// strace-like so grep works:
//
//	[3] config.resolve  @config.sig: false → true (CLI --sig)
//	[3] hook.before     ui.banner (global)
//	[3] atom.enter      fs.walk dirs=["."]
//	[3] atom.exit       fs.walk 123 files (45ms)
func (o *RunnerObserver) LogLifecycle(ctx context.Context, phase, msg string, fields ...any) {
	if o == nil || o.Verbosity() < VerbosityLifecycle {
		return
	}
	o.writeLine(VerbosityLifecycle, phase, msg, fields...)
}

// LogTiming emits per-node timing/counters. Cheaper than Lifecycle,
// fires on every atom but only when verbosity >= 2.
func (o *RunnerObserver) LogTiming(ctx context.Context, phase, msg string, fields ...any) {
	if o == nil || o.Verbosity() < VerbosityTiming {
		return
	}
	o.writeLine(VerbosityTiming, phase, msg, fields...)
}

// LogData emits payload inspection. Truncates aggressively — max 200
// chars per value, max 5 items per slice/map. Never dumps a 50MB blob
// to the terminal.
func (o *RunnerObserver) LogData(ctx context.Context, phase string, payload any) {
	if o == nil || o.Verbosity() < VerbosityData {
		return
	}
	out := o.Out()
	if out == nil {
		return
	}

	rendered := renderTruncated(payload, 200, 5)
	fmt.Fprintf(out, "[%d] %-16s %s\n", VerbosityData, phase, rendered)
}

func (o *RunnerObserver) writeLine(level int, phase, msg string, fields ...any) {
	out := o.Out()
	if out == nil {
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%d] %-16s %s", level, phase, msg)
	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(&sb, " %v=%v", fields[i], fields[i+1])
	}
	sb.WriteByte('\n')
	_, _ = io.WriteString(out, sb.String())
}

// renderTruncated converts v to a compact, bounded string representation.
// Strings are quoted and truncated; slices/maps are capped at maxItems.
func renderTruncated(v any, maxLen, maxItems int) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		return truncateString(x, maxLen)
	case []byte:
		return fmt.Sprintf("[]byte(%d)", len(x))
	case []string:
		return renderSlice(x, maxItems, maxLen)
	case map[string]any:
		return renderMap(x, maxItems, maxLen)
	}

	// Generic fallback: JSON encode, truncate the result.
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%T", v)
	}
	return truncateString(string(b), maxLen)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func renderSlice(xs []string, maxItems, maxLen int) string {
	if len(xs) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, x := range xs {
		if i >= maxItems {
			fmt.Fprintf(&sb, ", …(%d more)", len(xs)-maxItems)
			break
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(truncateString(x, maxLen))
	}
	sb.WriteByte(']')
	return sb.String()
}

func renderMap(m map[string]any, maxItems, maxLen int) string {
	if len(m) == 0 {
		return "{}"
	}
	var sb strings.Builder
	sb.WriteByte('{')
	i := 0
	for k, v := range m {
		if i >= maxItems {
			fmt.Fprintf(&sb, ", …(%d more)", len(m)-maxItems)
			break
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s=%s", k, renderTruncated(v, maxLen, maxItems))
		i++
	}
	sb.WriteByte('}')
	return sb.String()
}
