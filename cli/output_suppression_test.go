package cli

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestRunPreparedSourceSuppressesMarkedOutputAndPreservesAssertions(t *testing.T) {
	suppress := action.New("repopeek_cli_test.suppress", func(_ context.Context, _ any) (any, error) {
		return runner.SuppressOutput(map[string]any{"ok": true}), nil
	}).Build()
	cfg, err := runner.BuildConfig(append(native.Bundles(), compiler.Bundle{
		ID: "repopeek-cli-output-suppression-test",
		Libraries: []action.Library{{
			Name:    "repopeek_cli_test",
			Actions: []action.AnyAction{suppress},
		}},
	}))
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := "@assert: result.ok == true\nrepopeek_cli_test.suppress"
	for _, flags := range [][]string{nil, {"-v"}} {
		output := captureStdout(t, func() {
			code := runPreparedSource(t.Context(), cfg, source, "suppressed.nflow", nil, flags, runner.DurabilityOptions{}, false)
			if code != 0 {
				t.Fatalf("runPreparedSource returned %d for flags %v", code, flags)
			}
		})
		if output != "" {
			t.Fatalf("suppressed result was written to stdout for flags %v: %q", flags, output)
		}
	}
}
