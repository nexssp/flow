package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/nexssp/kernel/xctx"
)

type goDiagnostics struct {
	mu   sync.Mutex
	seen map[string]bool
}

var goDiagnosticsKey = xctx.NewKey[*goDiagnostics]("flow.builder.goDiagnostics")

// WithGoDiagnostics starts a fresh warning scope. Call once per CLI invocation,
// sharing the returned context across all dependency-resolution and build steps.
func WithGoDiagnostics(ctx context.Context) context.Context {
	return goDiagnosticsKey.With(ctx, &goDiagnostics{seen: make(map[string]bool)})
}

// goWarningWriter keeps only one bounded line at a time. Routine Go output is
// captured separately for errors; only concise warnings reach the quiet console.
type goWarningWriter struct {
	state   *goDiagnostics
	out     io.Writer
	pending []byte
}

func newGoWarningWriter(ctx context.Context, out io.Writer) *goWarningWriter {
	state, ok := goDiagnosticsKey.From(ctx)
	if !ok {
		state = &goDiagnostics{seen: make(map[string]bool)}
	}
	return &goWarningWriter{state: state, out: out}
}

func (w *goWarningWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		line, rest, complete := bytes.Cut(p, []byte{'\n'})
		remaining := maxCapturedOutputBytes - len(w.pending)
		w.pending = append(w.pending, line[:min(len(line), remaining)]...)
		if !complete {
			break
		}
		w.Flush()
		p = rest
	}
	return n, nil
}

func (w *goWarningWriter) Flush() {
	line := strings.TrimSuffix(string(w.pending), "\r")
	w.pending = w.pending[:0]
	message, ok := strings.CutPrefix(line, "go: warning: ")
	if !ok {
		return
	}
	key := message
	if module, reason, retracted := strings.Cut(message, ": retracted by module author: "); retracted {
		key = module
		message = module + " is retracted: " + reason
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if w.state.seen[key] {
		return
	}
	w.state.seen[key] = true
	_, _ = fmt.Fprintf(w.out, "nflow: warning: %s\n", message)
}
