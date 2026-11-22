package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"
)

// withStdin replaces os.Stdin with a pipe carrying input for the duration
// of the test. Cleanup restores the original fd.
func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	ktest.RequireNoError(t, err)
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
	})
}

func TestReadPipedStdin_JSONObjectMerged(t *testing.T) {
	withStdin(t, `{"user":"alice","count":42}`)

	payload := map[string]any{"existing": "kept"}
	got, err := readPipedStdin(payload)
	ktest.RequireNoError(t, err)

	user, ok := got["user"].(string)
	ktest.RequireCondition(t, ok, "user is %T, want string", got["user"])
	ktest.RequireEqual(t, user, "alice")

	count, ok := got["count"].(float64)
	ktest.RequireCondition(t, ok, "count is %T, want float64 (json.Unmarshal uses float64 for all numbers)", got["count"])
	ktest.RequireEqual(t, count, float64(42))

	existing, ok := got["existing"].(string)
	ktest.RequireCondition(t, ok, "existing is %T, want string", got["existing"])
	ktest.RequireEqual(t, existing, "kept")
}

func TestReadPipedStdin_ExistingKeysWin(t *testing.T) {
	withStdin(t, `{"user":"from-stdin"}`)

	payload := map[string]any{"user": "from-args"}
	got, err := readPipedStdin(payload)
	ktest.RequireNoError(t, err)

	ktest.RequireEqual(t, got["user"], "from-args")
}

func TestReadRunInput_PreserveStdin(t *testing.T) {
	const protocol = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	withStdin(t, protocol)

	payload, flags, err := readRunInput(map[string]any{"user": "alice"}, "runtime.noop", []string{preserveStdinFlag, "--json"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, payload, map[string]any{"user": "alice"})
	ktest.RequireEqual(t, flags, []string{"--json"})

	remaining, err := io.ReadAll(os.Stdin)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(remaining), protocol)
}

func TestRunSource_SuppressOutput(t *testing.T) {
	withStdin(t, "")
	out := captureStdout(t, func() {
		rc := runSourceInProcess(t.Context(), `"hidden"`, "suppress-output.nflow", []string{"--suppress-output"})
		ktest.RequireEqual(t, rc, 0)
	})
	ktest.RequireEqual(t, out, "")
}

func TestRunSource_SuppressOutputPreservesDirectStdout(t *testing.T) {
	withStdin(t, "")
	source := `"runner-result" -> runtime.print ( stream: "stdout", label: "direct-stdout" )`
	suppressed := captureStdout(t, func() {
		rc := runSourceInProcess(t.Context(), source, "direct-stdout.nflow", []string{"--json", "--suppress-output"})
		ktest.RequireEqual(t, rc, 0)
	})
	unsuppressed := captureStdout(t, func() {
		rc := runSourceInProcess(t.Context(), source, "direct-stdout.nflow", []string{"--json"})
		ktest.RequireEqual(t, rc, 0)
	})
	if !strings.Contains(suppressed, "[direct-stdout]") {
		t.Fatalf("suppress-output silenced direct stdout from the pipeline: %q", suppressed)
	}
	if !strings.HasPrefix(unsuppressed, suppressed) || len(unsuppressed) <= len(suppressed) {
		t.Fatalf("suppress-output did not remove only the runner's final result; suppressed=%q unsuppressed=%q", suppressed, unsuppressed)
	}
}

func TestReadPipedStdin_EmptyStdinIsNoop(t *testing.T) {
	withStdin(t, "")

	payload := map[string]any{"x": 1}
	got, err := readPipedStdin(payload)
	ktest.RequireNoError(t, err)

	ktest.RequireEqual(t, got, map[string]any{"x": 1})
}

func TestReadPipedStdin_NonJSONWrappedUnderInput(t *testing.T) {
	withStdin(t, "plain text payload\n")

	payload := map[string]any{}
	got, err := readPipedStdin(payload)
	ktest.RequireNoError(t, err)

	ktest.RequireEqual(t, got["input"], "plain text payload")
}

func TestReadPipedStdin_NonJSONAndExistingPayloadIsNoop(t *testing.T) {
	withStdin(t, "raw")

	payload := map[string]any{"user": "alice"}
	got, err := readPipedStdin(payload)
	ktest.RequireNoError(t, err)

	ktest.RequireEqual(t, got, map[string]any{"user": "alice"})
	if _, has := got["input"]; has {
		t.Fatal("non-JSON stdin must not be merged when payload is non-empty")
	}
}

func TestReadPipedStdin_NonObjectJSONWrappedUnderInput(t *testing.T) {
	withStdin(t, `[1,2,3]`)

	got, err := readPipedStdin(map[string]any{})
	ktest.RequireNoError(t, err)

	arr, ok := got["input"].([]any)
	ktest.RequireTrue(t, ok)
	ktest.RequireLen(t, arr, 3)
}

func TestRunSource_IoStdinFlow_KeepsStdinForStream(t *testing.T) {
	withStdin(t, "line-one\nline-two\n")

	src := `io.stdin -> collect`
	out := captureStdout(t, func() {
		rc := runSourceInProcess(t.Context(), src, "test.nflow", []string{"--json"})
		ktest.RequireEqual(t, rc, 0)
	})

	ktest.RequireStringContains(t, out, "line-one")
	ktest.RequireStringContains(t, out, "line-two")
}

func TestRunSource_NonStdinFlow_ConsumesStdinAsPayload(t *testing.T) {
	withStdin(t, `{"who":"stdin"}`)

	src := `{ who: .who }`
	out := captureStdout(t, func() {
		rc := runSourceInProcess(t.Context(), src, "test.nflow", []string{"--json"})
		ktest.RequireEqual(t, rc, 0)
	})

	ktest.RequireStringContains(t, out, `"who":"stdin"`)
}

// withOpenStdin replaces os.Stdin with a pipe whose writer stays open for the
// duration of the test: stdin is non-terminal, delivers no data, and never
// reaches EOF. This is the shape CI runners and containers provide.
func withOpenStdin(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	ktest.RequireNoError(t, err)
	t.Cleanup(func() {
		_ = w.Close()
		_ = r.Close()
	})
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}

// readPipedStdinDeadline runs fn and fails the test if it does not finish
// within the deadline, so a regression to an unbounded stdin read cannot hang
// the suite.
func readPipedStdinDeadline(t *testing.T, deadline time.Duration, fn func()) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		fn()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(deadline):
		t.Fatalf("readPipedStdin did not return within %s; open stdin is blocking the run again", deadline)
	}
}

func TestReadPipedStdin_OpenSilentStdinDoesNotBlock(t *testing.T) {
	withOpenStdin(t)
	t.Setenv("NFLOW_STDIN_WAIT", "20ms")

	payload := map[string]any{"x": 1}
	var got map[string]any
	var readErr error
	readPipedStdinDeadline(t, 5*time.Second, func() {
		got, readErr = readPipedStdin(payload)
	})
	ktest.RequireNoError(t, readErr)

	ktest.RequireEqual(t, got, map[string]any{"x": 1})
}

func TestReadPipedStdin_LiveSlowProducerStillWaitsForEOF(t *testing.T) {
	t.Setenv("NFLOW_STDIN_WAIT", "50ms")
	r, w, err := os.Pipe()
	ktest.RequireNoError(t, err)
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
		_ = w.Close()
	})

	// First byte is immediate; EOF arrives only after the first-byte bound has
	// passed. The read must still deliver the complete payload.
	_, err = w.WriteString(`{"user":"alice"}`)
	ktest.RequireNoError(t, err)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = w.Close()
	}()

	payload := map[string]any{"existing": "kept"}
	var got map[string]any
	var readErr error
	readPipedStdinDeadline(t, 5*time.Second, func() {
		got, readErr = readPipedStdin(payload)
	})
	ktest.RequireNoError(t, readErr)

	ktest.RequireEqual(t, got["user"], "alice")
	ktest.RequireEqual(t, got["existing"], "kept")
}

func TestStdinWait_InvalidValueWarnsAndUsesDefault(t *testing.T) {
	t.Setenv("NFLOW_STDIN_WAIT", "not-a-duration")

	wait := stdinWait()
	ktest.RequireEqual(t, wait, defaultStdinWait)
}

func TestStdinWait_ZeroWaitsIndefinitely(t *testing.T) {
	t.Setenv("NFLOW_STDIN_WAIT", "0s")
	ktest.RequireEqual(t, stdinWait(), time.Duration(0))
}

func TestReadStdinBounded_ImmediateEmptyEOFIsEmptyRead(t *testing.T) {
	withStdin(t, "")
	t.Setenv("NFLOW_STDIN_WAIT", "50ms")

	data, timedOut, err := readStdinBounded(os.Stdin, stdinWait())
	ktest.RequireNoError(t, err)
	ktest.RequireFalse(t, timedOut)
	ktest.RequireEqual(t, string(data), "")
}

type blockedStdinReader struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (r *blockedStdinReader) Read([]byte) (int, error) {
	close(r.started)
	defer close(r.finished)
	<-r.release
	return 0, io.EOF
}

func TestReadStdinBoundedTimeoutDoesNotRaceWithStdinReplacement(t *testing.T) {
	temporaryStdin, stdinWriter, err := os.Pipe()
	ktest.RequireNoError(t, err)
	oldStdin := os.Stdin
	os.Stdin = temporaryStdin
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = temporaryStdin.Close()
		_ = stdinWriter.Close()
	})

	reader := &blockedStdinReader{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	releaseReader := func() {
		select {
		case <-reader.release:
		default:
			close(reader.release)
		}
	}
	defer releaseReader()

	type result struct {
		timedOut bool
		err      error
	}
	got := make(chan result, 1)
	go func() {
		_, timedOut, readErr := readStdinBounded(reader, 20*time.Millisecond)
		got <- result{timedOut: timedOut, err: readErr}
	}()

	select {
	case <-reader.started:
	case <-time.After(5 * time.Second):
		t.Fatal("bounded reader did not use the supplied stdin reader")
	}
	select {
	case readResult := <-got:
		ktest.RequireNoError(t, readResult.err)
		ktest.RequireTrue(t, readResult.timedOut)
	case <-time.After(5 * time.Second):
		t.Fatal("bounded stdin read did not return after its deadline")
	}

	// Cleanup replaces os.Stdin while the timed-out read is still blocked.
	os.Stdin = oldStdin
	releaseReader()
	select {
	case <-reader.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked stdin read did not finish after release")
	}
}
