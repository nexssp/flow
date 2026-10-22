package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

func TestRunInputErrors_PreventWorkflowDispatch(t *testing.T) {
	for _, entry := range []string{"run", "implicit", "embedded", "factory"} {
		for _, input := range []string{"malformed JSON", "trailing JSON", "invalid after valid JSON", "unreadable stdin", "closed stdin"} {
			t.Run(entry+"/"+input, func(t *testing.T) {
				withStdin(t, "")
				args := []string{"--json"}
				wantError := "invalid JSON payload"
				switch input {
				case "malformed JSON":
					args = append(args, `{"partial":true,"broken":}`)
				case "trailing JSON":
					args = append(args, `{"ok":true} trailing`)
				case "invalid after valid JSON":
					args = append(args, `{"ok":true}`, `{"broken":}`)
				case "unreadable stdin":
					// A directory supports Stat but io.ReadAll fails on it.
					file, err := os.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					os.Stdin = file
					t.Cleanup(func() { _ = file.Close() })
					args = append(args, `{"ok":true}`)
					wantError = "read stdin:"
				case "closed stdin":
					if err := os.Stdin.Close(); err != nil {
						t.Fatal(err)
					}
					wantError = "stat stdin:"
				}

				var calls atomic.Int32
				probe := action.New("input_guard_probe.run", func(_ context.Context, value any) (any, error) {
					calls.Add(1)
					return value, nil
				}).Build()
				bundle := compiler.Bundle{
					ID:        "input_guard_probe",
					Libraries: []action.Library{{Name: "input_guard_probe", Actions: []action.AnyAction{probe}}},
				}
				const source = "input_guard_probe.run"
				code := 0
				stdout, stderr := captureIO(t, func() {
					switch entry {
					case "run", "implicit":
						path := filepath.Join(t.TempDir(), "input.nflow")
						if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
							t.Fatal(err)
						}
						cliArgs := append([]string{path}, args...)
						if entry == "run" {
							cliArgs = append([]string{"run"}, cliArgs...)
						}
						code = RunWithBundles(cliArgs, []compiler.Bundle{bundle})
					case "embedded":
						code = RunEmbeddedWithBundles(t.Context(), source, args, []compiler.Bundle{bundle})
					case "factory":
						code = RunEmbeddedWithBundleFactoriesForRequirements(t.Context(), source, args, nil, func(adopt func(compiler.Bundle) error) error {
							return adopt(bundle)
						})
					}
				})
				if code != 2 || calls.Load() != 0 || stdout != "" || !strings.Contains(stderr, wantError) {
					t.Fatalf("code=%d actions=%d stdout=%q stderr=%q; want code 2, no actions/output, and %q", code, calls.Load(), stdout, stderr, wantError)
				}
			})
		}
	}
}

func TestRunInput_ValidPaths(t *testing.T) {
	for _, test := range []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{name: "object argument", args: []string{`{"user":"argument"}`}, want: `{"user":"argument"}`},
		{name: "object stdin", stdin: `{"user":"stdin"}`, want: `{"user":"stdin"}`},
		{name: "argument keys win", args: []string{`{"user":"argument"}`}, stdin: `{"user":"stdin","extra":42}`, want: `{"user":"argument","extra":42}`},
		{name: "array stdin", stdin: `[1,2,3]`, want: `{"input":[1,2,3]}`},
		{name: "string stdin", stdin: `"hello"`, want: `{"input":"hello"}`},
		{name: "number stdin", stdin: `42`, want: `{"input":42}`},
		{name: "boolean stdin", stdin: `true`, want: `{"input":true}`},
		{name: "null stdin", stdin: `null`, want: `{}`},
		{name: "raw stdin", stdin: "plain text\n", want: `{"input":"plain text"}`},
		{name: "raw stdin with argument", args: []string{`{"user":"argument"}`}, stdin: "plain text", want: `{"user":"argument"}`},
		{name: "empty stdin", stdin: " \n\t", want: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			withStdin(t, test.stdin)
			var calls atomic.Int32
			probe := action.New("input_valid_probe.run", func(_ context.Context, value any) (any, error) {
				calls.Add(1)
				return value, nil
			}).Build()
			bundle := compiler.Bundle{
				ID:        "input_valid_probe",
				Libraries: []action.Library{{Name: "input_valid_probe", Actions: []action.AnyAction{probe}}},
			}
			code := 0
			stdout, stderr := captureIO(t, func() {
				code = RunEmbeddedWithBundles(t.Context(), "input_valid_probe.run", append(test.args, "--json"), []compiler.Bundle{bundle})
			})
			if code != 0 || calls.Load() != 1 {
				t.Fatalf("code=%d actions=%d stderr=%q; want success and one action", code, calls.Load(), stderr)
			}
			var got, want any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("decode stdout %q: %v", stdout, err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("output = %#v, want %#v", got, want)
			}
		})
	}
}
