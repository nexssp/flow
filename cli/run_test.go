package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

// captureStdout swaps os.Stdout for a pipe during fn and returns what
// was written. Required because runSourceInProcess writes directly to
// os.Stdout, and the info path is what we are testing.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	ktest.RequireNoError(t, err)

	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestRunSourceInProcess_InfoWithDirectives(t *testing.T) {
	// Source starts with @description, exactly like every fixture in
	// the tree. Before the fix, the info path parsed the raw source
	// and reported `unexpected token "description"` on line 1.
	src := `@description "test flow"
const @{ value: "hi" }
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}

func TestRunSourceInProcess_InfoWithMacros(t *testing.T) {
	// Source declares a macro and uses it. Info must parse cleanly
	// (requires the macro primary) and list the resulting atoms.
	src := `
@macro hi() { runtime.const @{ value: "hi" } }
@hi()
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}

func TestRunSourceInProcess_InfoWithPipeline(t *testing.T) {
	// A file whose entire body is a @pipeline directive. Clean output
	// is whitespace; info must still succeed.
	src := `
@pipeline p
  const @{ value: "x" }
@end
pipeline.p
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}
