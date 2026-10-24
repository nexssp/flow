package runner_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestRunSource_ReportsAssertionSourceLine(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `@assert {
  result.ok == false
}
const @{ value: { ok: true } }`
	var output bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, source, "assert-source.nflow", nil, runner.Opts{
		Stdout: &output,
		Stderr: &output,
	})
	if code != 1 {
		t.Fatalf("RunSource returned %d, want assertion failure (output: %s)", code, output.String())
	}
	if !strings.Contains(output.String(), "assert-source.nflow:2: result.ok == false") {
		t.Fatalf("failure output missing exact assertion source location:\n%s", output.String())
	}
}
