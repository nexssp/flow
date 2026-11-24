package progress

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xtest/ktest"
)

type concurrentWriteDetector struct {
	mu            sync.Mutex
	output        bytes.Buffer
	active        atomic.Int32
	maximum       atomic.Int32
	delayedStep   atomic.Bool
	spinnerStarts chan struct{}
	startOnce     sync.Once
}

func (w *concurrentWriteDetector) Write(p []byte) (int, error) {
	active := w.active.Add(1)
	for observed := w.maximum.Load(); active > observed; observed = w.maximum.Load() {
		if w.maximum.CompareAndSwap(observed, active) {
			break
		}
	}
	text := string(p)
	if strings.Contains(text, "Running task") && w.spinnerStarts != nil {
		w.startOnce.Do(func() { close(w.spinnerStarts) })
	}
	if strings.Contains(text, "step-") && w.delayedStep.CompareAndSwap(false, true) {
		// Keep one step write active across a spinner tick. Without reporter
		// write serialization the spinner goroutine enters Write concurrently.
		time.Sleep(180 * time.Millisecond)
	}
	w.mu.Lock()
	n, err := w.output.Write(p)
	w.mu.Unlock()
	w.active.Add(-1)
	return n, err
}

func (w *concurrentWriteDetector) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.String()
}

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

func TestReporter_TTYStepsStayVisibleAndSerializeWrites(t *testing.T) {
	writer := &concurrentWriteDetector{spinnerStarts: make(chan struct{})}
	r := newReporter(writer, true)
	defer r.Shutdown()

	r.Report(xctx.Progress{
		Message:  "Running task",
		Current:  0,
		Total:    1,
		Metadata: map[string]any{"kind": "wrap"},
	})
	select {
	case <-writer.spinnerStarts:
	case <-time.After(time.Second):
		t.Fatal("spinner did not render its first frame")
	}
	time.Sleep(5 * time.Millisecond)

	const stepCount = 8
	start := make(chan struct{})
	var reporters sync.WaitGroup
	for i := range stepCount {
		reporters.Go(func() {
			<-start
			r.Report(xctx.Progress{Message: "step-" + strconv.Itoa(i)})
		})
	}
	close(start)
	reporters.Wait()

	output := writer.String()
	for i := range stepCount {
		step := "step-" + strconv.Itoa(i)
		if !strings.Contains(output, "\x1b[32m✓\x1b[0m "+step+"\n\r\x1b[KRunning task  ") {
			t.Errorf("step %q was missing or not followed by a spinner redraw", step)
		}
	}
	if got := writer.maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent writer calls = %d, want 1", got)
	}
}
