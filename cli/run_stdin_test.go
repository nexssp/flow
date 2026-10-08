package cli

import (
	"os"
	"testing"

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
	got := readPipedStdin(payload)

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
	got := readPipedStdin(payload)

	ktest.RequireEqual(t, got["user"], "from-args")
}

func TestReadPipedStdin_EmptyStdinIsNoop(t *testing.T) {
	withStdin(t, "")

	payload := map[string]any{"x": 1}
	got := readPipedStdin(payload)

	ktest.RequireEqual(t, got, map[string]any{"x": 1})
}

func TestReadPipedStdin_NonJSONWrappedUnderInput(t *testing.T) {
	withStdin(t, "plain text payload\n")

	payload := map[string]any{}
	got := readPipedStdin(payload)

	ktest.RequireEqual(t, got["input"], "plain text payload")
}

func TestReadPipedStdin_NonJSONAndExistingPayloadIsNoop(t *testing.T) {
	withStdin(t, "raw")

	payload := map[string]any{"user": "alice"}
	got := readPipedStdin(payload)

	ktest.RequireEqual(t, got, map[string]any{"user": "alice"})
	if _, has := got["input"]; has {
		t.Fatal("non-JSON stdin must not be merged when payload is non-empty")
	}
}

func TestReadPipedStdin_NonObjectJSONWrappedUnderInput(t *testing.T) {
	withStdin(t, `[1,2,3]`)

	got := readPipedStdin(map[string]any{})

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
