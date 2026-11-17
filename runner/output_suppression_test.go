package runner_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func outputSuppressionConfig(t *testing.T) runner.Config {
	t.Helper()

	suppress := action.New("repopeek_test.suppress", func(_ context.Context, _ any) (any, error) {
		return runner.SuppressOutput(map[string]any{"ok": true}), nil
	}).Build()
	cfg, err := runner.BuildConfig(append(native.Bundles(), compiler.Bundle{
		ID: "repopeek-output-suppression-test",
		Libraries: []action.Library{{
			Name:    "repopeek_test",
			Actions: []action.AnyAction{suppress},
		}},
	}))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	return cfg
}

func TestExecuteUnwrapsSuppressedOutput(t *testing.T) {
	cfg := outputSuppressionConfig(t)
	ex, err := runner.Execute(t.Context(), cfg, "repopeek_test.suppress", "suppressed.nflow", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !ex.SuppressOutput {
		t.Fatal("Execution.SuppressOutput = false, want true")
	}
	got, ok := ex.Output.(map[string]any)
	if !ok {
		t.Fatalf("Execution.Output type = %T, want map[string]any", ex.Output)
	}
	if got["ok"] != true {
		t.Fatalf("Execution.Output = %#v, want unwrapped value", got)
	}
}

func TestRunSourceSuppressesMarkedOutputAndPreservesAssertionValue(t *testing.T) {
	cfg := outputSuppressionConfig(t)
	source := "@assert: result.ok == true\nrepopeek_test.suppress"

	for _, verbosity := range []int{0, 1} {
		t.Run(map[bool]string{true: "verbose", false: "normal"}[verbosity > 0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runner.RunSource(t.Context(), cfg, source, "suppressed.nflow", nil, runner.Opts{
				Stdout:    &stdout,
				Stderr:    &stderr,
				Verbosity: verbosity,
			})
			if code != 0 {
				t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
			}
			if verbosity == 0 && stderr.Len() != 0 {
				t.Fatalf("successful non-verbose suppression wrote to stderr: %q", stderr.String())
			}
			if verbosity > 0 && !strings.Contains(stderr.String(), "all assertions passed") {
				t.Fatalf("verbose run did not report successful assertions: %q", stderr.String())
			}
			if strings.Contains(stdout.String(), "map[ok:true]") || strings.Contains(stdout.String(), `"ok":true`) || strings.Contains(stdout.String(), `"ok": true`) {
				t.Fatalf("suppressed action result was rendered: %q", stdout.String())
			}
			if verbosity == 0 && stdout.Len() != 0 {
				t.Fatalf("non-verbose suppression wrote to stdout: %q", stdout.String())
			}
		})
	}
}
