package progress

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestReporter_StepNonTTY(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{
		Message:  "Loading",
		Current:  1,
		Total:    1,
		Metadata: map[string]any{"kind": "step"},
	})

	ktest.RequireEqual(t, buf.String(), "✓ Loading\n")
}

func TestReporter_WrapNonTTY(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{
		Message:  "Reading files",
		Current:  0,
		Total:    1,
		Metadata: map[string]any{"kind": "wrap"},
	})
	ktest.RequireStringContains(t, buf.String(), "▶ Reading files")

	r.Report(xctx.Progress{
		Message:  "Reading files",
		Current:  1,
		Total:    1,
		Metadata: map[string]any{"kind": "wrap"},
	})
	ktest.RequireStringContains(t, buf.String(), "✓ Reading files")
}

func TestReporter_WrapFailureNonTTY(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{Message: "Failing op", Current: 0, Total: 1, Metadata: map[string]any{"kind": "wrap"}})
	r.Report(xctx.Progress{
		Message:  "Failing op",
		Current:  1,
		Total:    1,
		Metadata: map[string]any{"kind": "wrap", "error": "boom"},
	})

	ktest.RequireStringContains(t, buf.String(), "✗ Failing op")
}

func TestReporter_BareProgressRendersAsStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{Message: "Hello"})
	ktest.RequireEqual(t, buf.String(), "✓ Hello\n")
}

func TestReporter_EmptyMessageIsIgnored(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{Metadata: map[string]any{"kind": "step"}})
	r.Report(xctx.Progress{Metadata: map[string]any{"kind": "wrap"}, Current: 0, Total: 1})

	ktest.RequireEqual(t, buf.String(), "")
}

func TestReporter_ShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	r.Shutdown()
	r.Shutdown()

	r.Report(xctx.Progress{Message: "after shutdown"})
	ktest.RequireEqual(t, buf.String(), "")
}

func TestReporter_StartAfterDoneNoActiveStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	// Done without a prior start — e.g. an action that emitted only
	// its final event. Must not panic.
	r.Report(xctx.Progress{
		Message:  "Orphan done",
		Current:  1,
		Total:    1,
		Metadata: map[string]any{"kind": "wrap"},
	})
	ktest.RequireStringContains(t, buf.String(), "✓ Orphan done")
}

func TestReporter_NewStartClosesPreviousWrap(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	defer r.Shutdown()

	r.Report(xctx.Progress{Message: "A", Current: 0, Total: 1, Metadata: map[string]any{"kind": "wrap"}})
	r.Report(xctx.Progress{Message: "B", Current: 0, Total: 1, Metadata: map[string]any{"kind": "wrap"}})

	// Both labels must appear in the output; there is no goroutine leak
	// (previous spinner is stopped).
	out := buf.String()
	ktest.RequireStringContains(t, out, "▶ A")
	ktest.RequireStringContains(t, out, "▶ B")
}

func TestReporter_TTYDetection(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := NewReporter(&buf)
	ktest.RequireFalse(t, r.tty)
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatal("non-TTY reporter must not emit ANSI escapes")
	}
}
