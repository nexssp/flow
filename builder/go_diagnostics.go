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

// goWarningWriter captures warnings for concise output. In verbose mode it also
// forwards non-warning stderr unchanged while keeping warning lines bounded.
type goWarningWriter struct {
	state       *goDiagnostics
	out         io.Writer
	pending     []byte
	verbose     bool
	passthrough bool
	warningLine bool
	truncated   bool
}

func newGoWarningWriter(ctx context.Context, out io.Writer) *goWarningWriter {
	return newGoWarningWriterMode(ctx, out, false)
}

func newGoWarningWriterMode(ctx context.Context, out io.Writer, verbose bool) *goWarningWriter {
	state, ok := goDiagnosticsKey.From(ctx)
	if !ok {
		state = &goDiagnostics{seen: make(map[string]bool)}
	}
	return &goWarningWriter{state: state, out: out, verbose: verbose}
}

func (w *goWarningWriter) Write(p []byte) (int, error) {
	if !w.verbose {
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

	written := len(p)
	const warningPrefix = "go: warning: "
	prefix := []byte(warningPrefix)
	for len(p) > 0 {
		if w.passthrough {
			newline := bytes.IndexByte(p, '\n')
			if newline < 0 {
				_, err := w.out.Write(p)
				return written, err
			}
			if _, err := w.out.Write(p[:newline+1]); err != nil {
				return written, err
			}
			p = p[newline+1:]
			w.passthrough = false
			continue
		}

		line, rest, complete := bytes.Cut(p, []byte{'\n'})
		if w.warningLine {
			w.appendWarningBytes(line)
			if complete {
				w.flushWarning()
				p = rest
				continue
			}
			return written, nil
		}

		candidate := w.pending
		take := min(len(line), len(prefix)-len(candidate))
		candidate = append(candidate, line[:take]...)
		if bytes.HasPrefix(prefix, candidate) && len(candidate) < len(prefix) && !complete {
			w.pending = candidate
			return written, nil
		}
		if len(candidate) == len(prefix) && bytes.Equal(candidate, prefix) {
			w.warningLine = true
			w.pending = append(w.pending[:0], candidate...)
			w.truncated = false
			w.appendWarningBytes(line[take:])
			if complete {
				w.flushWarning()
				p = rest
				continue
			}
			return written, nil
		}

		if len(w.pending) > 0 {
			if _, err := w.out.Write(w.pending); err != nil {
				return written, err
			}
			w.pending = w.pending[:0]
		}
		if len(line) > 0 {
			if _, err := w.out.Write(line); err != nil {
				return written, err
			}
		}
		if complete {
			if _, err := w.out.Write([]byte{'\n'}); err != nil {
				return written, err
			}
			p = rest
			continue
		}
		w.passthrough = true
		return written, nil
	}
	return written, nil
}

func (w *goWarningWriter) appendWarningBytes(p []byte) {
	remaining := maxCapturedOutputBytes - len(w.pending)
	if remaining > 0 {
		w.pending = append(w.pending, p[:min(len(p), remaining)]...)
	}
	if len(p) > max(remaining, 0) {
		w.truncated = true
	}
}

func (w *goWarningWriter) flushWarning() {
	line := strings.TrimSuffix(string(w.pending), "\r")
	truncated := w.truncated
	w.pending = w.pending[:0]
	w.warningLine = false
	w.truncated = false
	message, ok := strings.CutPrefix(line, "go: warning: ")
	if !ok {
		return
	}
	key := message
	if module, reason, retracted := strings.Cut(message, ": retracted by module author: "); retracted {
		key = module
		message = module + " is retracted: " + reason
	}
	if truncated {
		message += "… (warning truncated)"
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if w.state.seen[key] {
		return
	}
	w.state.seen[key] = true
	_, _ = fmt.Fprintf(w.out, "nflow: warning: %s\n", message)
}

func (w *goWarningWriter) Flush() {
	if w.verbose {
		if w.warningLine {
			w.flushWarning()
			return
		}
		if len(w.pending) > 0 {
			_, _ = w.out.Write(w.pending)
			w.pending = w.pending[:0]
		}
		return
	}

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
