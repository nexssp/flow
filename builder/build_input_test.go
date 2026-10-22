package builder

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBuild_StandaloneJSONInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping standalone build integration test in short mode")
	}

	workDir := t.TempDir()
	bundleDir := filepath.Join(workDir, "probe")
	ktest.RequireNoError(t, os.MkdirAll(bundleDir, 0o755))
	const bundleSource = `package probe

import (
	"context"
	"os"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

func Bundle(_ map[string]string) compiler.Bundle {
	act := action.New("probe.mark", func(_ context.Context, input map[string]any) (map[string]any, error) {
		if err := os.WriteFile(os.Getenv("NFLOW_INPUT_TEST_MARKER"), []byte("executed"), 0o600); err != nil {
			return nil, err
		}
		return input, nil
	}).Build()
	return compiler.Bundle{
		ID: "probe",
		Libraries: []action.Library{{Name: "probe", Actions: []action.AnyAction{act}}},
	}
}
`
	ktest.RequireNoError(t, os.WriteFile(filepath.Join(bundleDir, "library.go"), []byte(bundleSource), 0o600))
	flowPath := filepath.Join(workDir, "input.nflow")
	ktest.RequireNoError(t, os.WriteFile(flowPath, []byte("@require ./probe as probe\nprobe.mark\n"), 0o600))
	binaryPath := filepath.Join(workDir, "input-flow"+exeSuffix())
	ktest.RequireEqual(t, runBuild([]string{flowPath, "-o", binaryPath}), 0)

	cases := []struct {
		name    string
		args    []string
		stdin   string
		invalid bool
		want    map[string]any
	}{
		{name: "truncated_object", args: []string{`{"value":`}, invalid: true},
		{name: "trailing_comma", args: []string{`{"value":1,}`}, invalid: true},
		{name: "invalid_token", args: []string{`{"value":NaN}`}, invalid: true},
		{name: "invalid_value_type", args: []string{`{"value":1e1000}`}, invalid: true},
		{name: "trailing_text", args: []string{`{"value":1}garbage`}, invalid: true},
		{name: "second_document", args: []string{`{"value":1} {"other":2}`}, invalid: true},
		{name: "valid_then_invalid", args: []string{`{"value":1}`, `{"other":`}, invalid: true},
		{name: "invalid_then_valid", args: []string{`{"value":`, `{"other":2}`}, invalid: true},
		{name: "json_flag_invalid", args: []string{"--json", `{"value":`}, invalid: true},
		{name: "valid_object", args: []string{`{"value":"ok","nested":{"n":2}}`}, want: map[string]any{"value": "ok", "nested": map[string]any{"n": float64(2)}}},
		{name: "trailing_whitespace", args: []string{"{\"value\":\"ok\"} \n\t"}, want: map[string]any{"value": "ok"}},
		{name: "multiple_objects", args: []string{`{"value":1}`, `{"other":2}`}, want: map[string]any{"value": float64(1), "other": float64(2)}},
		{name: "json_flag_valid", args: []string{"--json", `{"value":"ok"}`}, want: map[string]any{"value": "ok"}},
		{name: "empty_payload", want: map[string]any{}},
		{name: "raw_argument_unchanged", args: []string{"raw text"}, want: map[string]any{}},
		{name: "raw_stdin_unchanged", stdin: "raw text\n", want: map[string]any{}},
		{name: "json_like_stdin_unchanged", stdin: `{"value":`, want: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "executed")
			cmd := exec.CommandContext(t.Context(), binaryPath, tc.args...) //nolint:gosec // Runs only the fixture binary built above with fixed test arguments.
			cmd.Env = append(os.Environ(), "NFLOW_INPUT_TEST_MARKER="+marker)
			cmd.Stdin = strings.NewReader(tc.stdin)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if tc.invalid {
				if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok {
					t.Errorf("expected nonzero process exit, got %v; stdout=%s stderr=%s", err, &stdout, &stderr)
				} else {
					ktest.RequireEqual(t, exitErr.ExitCode(), 2)
				}
				_, statErr := os.Stat(marker)
				ktest.RequireCondition(t, errors.Is(statErr, os.ErrNotExist), "workflow action executed before input rejection; marker stat: %v", statErr)
				ktest.RequireEqual(t, stdout.String(), "")
				ktest.RequireStringContains(t, stderr.String(), "invalid JSON payload:")
				return
			}
			if err != nil {
				t.Fatalf("standalone failed: %v; stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			mark, readErr := os.ReadFile(marker)
			ktest.RequireNoError(t, readErr)
			ktest.RequireEqual(t, string(mark), "executed")
			var got map[string]any
			ktest.RequireNoError(t, json.Unmarshal(stdout.Bytes(), &got))
			ktest.RequireEqual(t, got, tc.want)
			if len(tc.args) > 0 && tc.args[0] == "--json" {
				ktest.RequireCondition(t, !strings.Contains(stderr.String(), "(built "), "--json printed a version banner: %s", &stderr)
			}
		})
	}
}
