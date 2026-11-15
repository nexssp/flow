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
	if w.verbose {
		return w.writeVerbose(p)
	}
	return w.writeConcise(p)
}

func (w *goWarningWriter) writeConcise(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		line, rest, complete := bytes.Cut(p, []byte{'\n'})
		remaining := maxCapturedOutputBytes - len(w.pending)
		w.pending = append(w.pending, line[:min(len(line), remaining)]...)
		if !complete {
			break
		}
		if err := w.flushConciseLine(); err != nil {
			return written, err
		}
		p = rest
	}
	return written, nil
}

func (w *goWarningWriter) writeVerbose(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		rest, err := w.writeVerboseChunk(p)
		if err != nil {
			return written, err
		}
		p = rest
	}
	return written, nil
}

func (w *goWarningWriter) writeVerboseChunk(p []byte) ([]byte, error) {
	if w.passthrough {
		return w.writeVerbosePassthrough(p)
	}

	line, rest, complete := bytes.Cut(p, []byte{'\n'})
	if w.warningLine {
		return w.writeVerboseWarningChunk(line, rest, complete)
	}

	const warningPrefix = "go: warning: "
	prefix := []byte(warningPrefix)
	candidate := w.pending
	take := min(len(line), len(prefix)-len(candidate))
	candidate = append(candidate, line[:take]...)
	if bytes.HasPrefix(prefix, candidate) && len(candidate) < len(prefix) && !complete {
		w.pending = candidate
		return nil, nil
	}
	if len(candidate) == len(prefix) && bytes.Equal(candidate, prefix) {
		w.warningLine = true
		w.pending = append(w.pending[:0], candidate...)
		w.truncated = false
		w.appendWarningBytes(line[take:])
		if complete {
			if err := w.flushWarning(); err != nil {
				return rest, err
			}
			return rest, nil
		}
		return nil, nil
	}

	if err := w.writeVerboseOrdinaryLine(line, complete); err != nil {
		return nil, err
	}
	if complete {
		return rest, nil
	}
	w.passthrough = true
	return nil, nil
}

func (w *goWarningWriter) writeVerbosePassthrough(p []byte) ([]byte, error) {
	newline := bytes.IndexByte(p, '\n')
	if newline < 0 {
		return nil, writeFull(w.out, p)
	}
	if err := writeFull(w.out, p[:newline+1]); err != nil {
		return nil, err
	}
	w.passthrough = false
	return p[newline+1:], nil
}

func (w *goWarningWriter) writeVerboseWarningChunk(line, rest []byte, complete bool) ([]byte, error) {
	w.appendWarningBytes(line)
	if !complete {
		return nil, nil
	}
	if err := w.flushWarning(); err != nil {
		return rest, err
	}
	return rest, nil
}

func (w *goWarningWriter) writeVerboseOrdinaryLine(line []byte, complete bool) error {
	if len(w.pending) > 0 {
		if err := writeFull(w.out, w.pending); err != nil {
			return err
		}
		w.pending = w.pending[:0]
	}
	if len(line) > 0 {
		if err := writeFull(w.out, line); err != nil {
			return err
		}
	}
	if complete {
		return writeFull(w.out, []byte{'\n'})
	}
	return nil
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

func (w *goWarningWriter) flushWarning() error {
	line := strings.TrimSuffix(string(w.pending), "\r")
	truncated := w.truncated
	w.pending = w.pending[:0]
	w.warningLine = false
	w.truncated = false
	return w.emitWarningLine(line, truncated)
}

func (w *goWarningWriter) flushConciseLine() error {
	line := strings.TrimSuffix(string(w.pending), "\r")
	w.pending = w.pending[:0]
	return w.emitWarningLine(line, false)
}

func (w *goWarningWriter) emitWarningLine(line string, truncated bool) error {
	message, ok := strings.CutPrefix(line, "go: warning: ")
	if !ok {
		return nil
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
		return nil
	}
	w.state.seen[key] = true
	_, err := fmt.Fprintf(w.out, "nflow: warning: %s\n", message)
	return err
}

func (w *goWarningWriter) Flush() error {
	if !w.verbose {
		return w.flushConciseLine()
	}
	if w.warningLine {
		return w.flushWarning()
	}
	if len(w.pending) == 0 {
		return nil
	}
	err := writeFull(w.out, w.pending)
	w.pending = w.pending[:0]
	return err
}

func writeFull(out io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := out.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(p) {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
