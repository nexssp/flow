package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestThinAndCLIExecutionFailureDiagnosticsMatch(t *testing.T) {
	fail := action.New("diagnostics_test.fail", func(context.Context, any) (any, error) {
		return nil, errors.New("planned execution failure")
	}).Build()
	bundles := append(native.Bundles(), compiler.Bundle{
		ID:        "diagnostics_test",
		Libraries: []action.Library{{Name: "diagnostics_test", Actions: []action.AnyAction{fail}}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	name := filepath.Join(t.TempDir(), "execution-failure.nflow")
	source := "# prior context\n{} -> diagnostics_test.fail\n# following context\n"
	if err := os.WriteFile(name, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	cliCode, cliStdout, cliStderr := runCLIForThinDiagnosticTest(t, cfg, source, name, false)
	thinCode, thinStdout, thinStderr := runThinForDiagnosticTest(t, cfg, source, name, false)
	if cliCode == 0 || thinCode == 0 {
		t.Fatalf("expected both runners to fail; CLI=%d thin=%d", cliCode, thinCode)
	}
	if cliStdout != thinStdout || cliStderr != thinStderr {
		t.Fatalf("diagnostics differ\nCLI stdout: %q\nthin stdout: %q\nCLI stderr: %q\nthin stderr: %q", cliStdout, thinStdout, cliStderr, thinStderr)
	}
	for _, want := range []string{"Pipeline Execution Failed", "planned execution failure"} {
		if !strings.Contains(thinStderr, want) {
			t.Errorf("thin stderr missing %q:\n%s", want, thinStderr)
		}
	}

	_, _, verboseCLI := runCLIForThinDiagnosticTest(t, cfg, source, name, true)
	_, _, verboseThin := runThinForDiagnosticTest(t, cfg, source, name, true)
	for label, output := range map[string]string{"CLI": verboseCLI, "thin": verboseThin} {
		for _, want := range []string{"Pipeline Execution Failed", "EXECUTION TRACE", "diagnostics_test.fail"} {
			if !strings.Contains(output, want) {
				t.Errorf("verbose %s stderr missing %q:\n%s", label, want, output)
			}
		}
	}
}

func TestThinAndCLIPositionedSourceErrorDiagnosticsMatch(t *testing.T) {
	name := filepath.Join(t.TempDir(), "positioned-error.nflow")
	source := "# prior context\n{} -> diagnostics_test.positioned\n# following context\n"
	if err := os.WriteFile(name, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	positioned := action.New("diagnostics_test.positioned", func(context.Context, any) (any, error) {
		return nil, compiler.SourceError(compiler.Position{File: name, Line: 2, Col: 1}, "positioned execution failure")
	}).Build()
	bundles := append(native.Bundles(), compiler.Bundle{
		ID:        "diagnostics_test",
		Libraries: []action.Library{{Name: "diagnostics_test", Actions: []action.AnyAction{positioned}}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	cliCode, cliStdout, cliStderr := runCLIForThinDiagnosticTest(t, cfg, source, name, false)
	thinCode, thinStdout, thinStderr := runThinForDiagnosticTest(t, cfg, source, name, false)
	if cliCode == 0 || thinCode == 0 {
		t.Fatalf("expected both runners to fail; CLI=%d thin=%d", cliCode, thinCode)
	}
	if cliStdout != thinStdout || cliStderr != thinStderr {
		t.Fatalf("positioned diagnostics differ\nCLI stdout: %q\nthin stdout: %q\nCLI stderr: %q\nthin stderr: %q", cliStdout, thinStdout, cliStderr, thinStderr)
	}
	for _, want := range []string{"Pipeline Execution Failed", "positioned execution failure", "2 │ {} -> diagnostics_test.positioned", "^"} {
		if !strings.Contains(thinStderr, want) {
			t.Errorf("thin stderr missing %q:\n%s", want, thinStderr)
		}
	}
}

func runCLIForThinDiagnosticTest(t *testing.T, cfg runner.Config, source, name string, verbose bool) (exitCode int, capturedStdout, capturedStderr string) {
	t.Helper()
	flags := []string{"--json"}
	if verbose {
		flags = append(flags, "-v")
	}
	code := 0
	stdout, stderr := captureIO(t, func() {
		code = runPreparedSource(t.Context(), cfg, source, name, nil, flags, runner.DurabilityOptions{})
	})
	return code, stdout, stderr
}

func runThinForDiagnosticTest(t *testing.T, cfg runner.Config, source, name string, verbose bool) (exitCode int, capturedStdout, capturedStderr string) {
	t.Helper()
	var stdout, stderr strings.Builder
	verbosity := 0
	if verbose {
		verbosity = 1
	}
	code := runner.RunSource(t.Context(), cfg, source, name, nil, runner.Opts{
		Verbosity: verbosity,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	return code, stdout.String(), stderr.String()
}
