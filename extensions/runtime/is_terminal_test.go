package runtime

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/internal/terminal"
	"github.com/nexssp/flow/runner"
)

func TestIsTerminalReportsPipeAsNonTerminal(t *testing.T) {
	reader, writer, err := os.Pipe()
	ktest.RequireNoError(t, err)
	original := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
		_ = writer.Close()
		_ = reader.Close()
	})

	got, err := IsTerminal.DoAny(context.Background(), nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, got, false)
}

func TestIsTerminalMatchesCurrentStdout(t *testing.T) {
	got, err := IsTerminal.DoAny(context.Background(), nil)
	ktest.RequireNoError(t, err)
	value, ok := got.(bool)
	ktest.RequireCondition(t, ok, "IsTerminal result type = %T, want bool", got)
	ktest.RequireEqual(t, value, terminal.IsTerminal(os.Stdout))
}

func TestIsTerminalFixture(t *testing.T) {
	source, err := fs.ReadFile(Bundle(nil).Fixtures, "nflows/is_terminal.nflow")
	ktest.RequireNoError(t, err)

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		Bundle(nil),
		assert.Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, string(source), "is_terminal.nflow", nil, runner.Opts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != 0 {
		t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
	}
	ktest.RequireEqual(t, stderr.String(), "")
}
