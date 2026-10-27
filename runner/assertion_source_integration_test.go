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
	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, source, "assert-source.nflow", nil, runner.Opts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != 1 {
		t.Fatalf("RunSource returned %d, want assertion failure (stdout: %s; stderr: %s)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "assert-source.nflow:2: result.ok == false") {
		t.Fatalf("stderr missing exact assertion source location:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "assert-source.nflow:2:") || strings.Contains(stdout.String(), "exit status: 1") {
		t.Fatalf("assertion failure polluted stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) {
		t.Fatalf("stdout missing flow result:\n%s", stdout.String())
	}
}

func TestRunSource_ExecutionErrorsUseStderr(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, "{} -> missing.action", "missing.nflow", nil, runner.Opts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code == 0 {
		t.Fatal("RunSource succeeded for an unknown action")
	}
	if stdout.Len() != 0 {
		t.Fatalf("execution error polluted stdout: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("execution error was not written to stderr")
	}
}
