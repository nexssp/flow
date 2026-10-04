package macros

import (
	"context"
	"sync"

	"github.com/nexssp/kernel/xctx"
)

var traceKey = xctx.NewKey[*Trace]("macros.trace")

// Expansion is one recorded macro invocation and its substituted body.
// DefLine and InvokeLine are 1-based. For an expansion that occurred at
// the top level, InvokeLine is file-relative. For an expansion inside a
// pipeline body, InvokeLine is body-relative and Where names the
// pipeline.
type Expansion struct {
	Macro      string
	DefLine    int
	InvokeLine int
	Where      string // "top-level" or "pipeline <name>"
	Body       string
	Err        error
}

// Trace records every macro expansion that occurs during one parse.
// Install it into the parser's context before Parse; if no trace is
// installed, expansion runs without recording anything.
type Trace struct {
	mu         sync.Mutex
	expansions []Expansion
}

// WithTrace installs a trace into ctx. A nil trace is a no-op.
func WithTrace(ctx context.Context, t *Trace) context.Context {
	if t == nil {
		return ctx
	}
	return traceKey.With(ctx, t)
}

// TraceFrom returns the trace installed in ctx, or nil.
func TraceFrom(ctx context.Context) *Trace {
	t, _ := traceKey.From(ctx)
	return t
}

// Record appends one expansion.
func (t *Trace) Record(e Expansion) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.expansions = append(t.expansions, e)
	t.mu.Unlock()
}

// Expansions returns a copy of the recorded expansions, in the order
// they completed.
func (t *Trace) Expansions() []Expansion {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Expansion, len(t.expansions))
	copy(out, t.expansions)
	return out
}
