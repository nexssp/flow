package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/flow/builder"
	"github.com/nexssp/flow/extensions/require"
)

func TestThinHarnessArgsSupported(t *testing.T) {
	const path = "/tmp/workflow.nflow"
	tests := []struct {
		name       string
		subcommand string
		args       []string
		want       bool
	}{
		{name: "plain run", subcommand: "run", args: []string{path}, want: true},
		{name: "JSON payload", subcommand: "run", args: []string{path, `{"value":1}`}, want: true},
		{name: "JSON output flag", subcommand: "run", args: []string{path, "--json"}, want: true},
		{name: "payload and JSON output", subcommand: "run", args: []string{path, `{"value":1}`, "--json"}, want: true},
		{name: "extra run flags", subcommand: "run", args: []string{path, "--info"}, want: false},
		{name: "malformed JSON payload", subcommand: "run", args: []string{path, `{"value":`}, want: false},
		{name: "multiple payloads", subcommand: "run", args: []string{path, `{}`, `{}`}, want: false},
		{name: "flag before path", subcommand: "run", args: []string{"--json", path}, want: false},
		{name: "other command", subcommand: "show", args: []string{path}, want: false},
		{name: "path mismatch", subcommand: "run", args: []string{"other.nflow"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := thinHarnessArgsSupported(test.subcommand, path, test.args); got != test.want {
				t.Fatalf("thinHarnessArgsSupported(%q, %q, %#v) = %t, want %t", test.subcommand, path, test.args, got, test.want)
			}
		})
	}
}

func TestRunHarnessWithFallback(t *testing.T) {
	tests := []struct {
		name         string
		thinSelected bool
		thinExit     int
		legacyExit   int
		wantExit     int
		wantCalls    []string
	}{
		{
			name:         "thin refusal runs CLI harness",
			thinSelected: true,
			thinExit:     builder.HarnessFallbackExitCode,
			legacyExit:   0,
			wantExit:     0,
			wantCalls:    []string{"exec:thin", "ensure:cli", "exec:cli"},
		},
		{
			name:         "thin execution failure is returned",
			thinSelected: true,
			thinExit:     1,
			legacyExit:   0,
			wantExit:     1,
			wantCalls:    []string{"exec:thin"},
		},
		{
			name:         "CLI exit 125 is not retried",
			thinSelected: false,
			thinExit:     builder.HarnessFallbackExitCode,
			legacyExit:   0,
			wantExit:     builder.HarnessFallbackExitCode,
			wantCalls:    []string{"exec:thin"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			got := runHarnessWithFallback(
				context.Background(), "thin", []string{"run", "flow.nflow"}, test.thinSelected,
				nil, "flow.nflow",
				func(_ context.Context, _ []require.Requirement, path ...string) (string, error) {
					calls = append(calls, "ensure:cli")
					if len(path) != 1 || path[0] != "flow.nflow" {
						t.Fatalf("CLI ensure path = %v, want flow.nflow", path)
					}
					return "cli", nil
				},
				func(_ context.Context, bin string, args []string) int {
					calls = append(calls, "exec:"+bin)
					if !reflect.DeepEqual(args, []string{"run", "flow.nflow"}) {
						t.Fatalf("execute args = %v", args)
					}
					if bin == "thin" {
						return test.thinExit
					}
					return test.legacyExit
				},
			)
			if got != test.wantExit {
				t.Fatalf("runHarnessWithFallback exit = %d, want %d", got, test.wantExit)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, test.wantCalls)
			}
		})
	}
}

func TestGeneratedThinHarnessFallbackRunsCLIWithPipedStdin(t *testing.T) {
	workDir := t.TempDir()
	helperDir := filepath.Join(workDir, "helpers")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helperSource := `package helpers

import (
	"context"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

const ID = "helpers"

func init() { compiler.Register(ID, Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: ID,
			Actions: []action.AnyAction{
				action.New("helpers.echo", func(context.Context, any) (map[string]any, error) {
					return map[string]any{"value": "ok"}, nil
				}).Build(),
			},
		}},
	}
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "library.go"), []byte(helperSource), 0o600); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(workDir, "test.nflow")
	flow := `@require ./helpers
@assert: result.value == "ok"
{} -> helpers.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}
	reqs, err := builder.SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatalf("read @require declarations: %v", err)
	}
	t.Setenv("NFLOW_HARNESS_CACHE", t.TempDir())
	thinBin, err := builder.EnsureThinHarnessContext(t.Context(), reqs, flowPath)
	if err != nil {
		t.Fatalf("build thin harness: %v", err)
	}

	stdin, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin, oldStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdoutWriter
	defer func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutWriter.Close()
	}()

	code := runHarnessWithFallback(
		t.Context(), thinBin, []string{"run", flowPath, "--json"}, true, reqs, flowPath,
		builder.EnsureHarnessCLIContext, builder.ExecHarnessContext,
	)
	_ = stdoutWriter.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("read fallback stdout: %v", err)
	}
	if code != 0 {
		t.Fatalf("thin refusal and CLI fallback returned %d; stdout: %s", code, output)
	}
	if !bytes.Contains(output, []byte(`"value":"ok"`)) && !strings.Contains(string(output), `"value": "ok"`) {
		t.Fatalf("fallback output missing the expected result: %s", output)
	}
}
