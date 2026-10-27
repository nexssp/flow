package lang

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRunCmdUsesDepsTemplateAndFreshFilePath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(exe))
	tool := filepath.Base(exe)
	lang := &Language{
		ID: "python",
		spec: &RunnerSpec{
			ID:         "python",
			Version:    "3",
			Deps:       tool + " run {file}",
			DepsMarker: "# /// script",
		},
		Select: func(string) (string, []string, error) {
			t.Fatal("ordinary interpreter should not be selected for a PEP 723 script")
			return "", nil, nil
		},
	}
	block := &LangBlock{ID: "python", Version: "3", Source: "# /// script\nprint(1)"}
	wantCmd, err := lookPathCmd(tool)
	if err != nil {
		t.Fatal(err)
	}

	for _, filePath := range []string{"/tmp/first path.py", "/tmp/second path.py"} {
		cmd, args, err := resolveRunCmd(context.Background(), lang, block, filePath)
		if err != nil {
			t.Fatalf("resolveRunCmd: %v", err)
		}
		if cmd != wantCmd {
			t.Errorf("cmd = %q; want %q", cmd, wantCmd)
		}
		if len(args) != 2 || args[0] != "run" || args[1] != filePath {
			t.Errorf("args = %#v; want [run %q]", args, filePath)
		}
	}
}

func TestResolveRunCmdUsesPlainInterpreterWithoutMarker(t *testing.T) {
	lang := &Language{
		ID:   "python",
		spec: &RunnerSpec{ID: "python", Deps: "uv run {file}", DepsMarker: "# /// script"},
		Select: func(string) (string, []string, error) {
			return "python3", []string{"-3"}, nil
		},
	}
	block := &LangBlock{ID: "python", Source: "print(1)"}
	cmd, args, err := resolveRunCmd(context.Background(), lang, block, "/tmp/script.py")
	if err != nil {
		t.Fatalf("resolveRunCmd: %v", err)
	}
	if cmd != "python3" || len(args) != 2 || args[0] != "-3" || args[1] != "/tmp/script.py" {
		t.Errorf("got %q %#v; want python3 [-3 /tmp/script.py]", cmd, args)
	}
}

func TestPreflightChecksDependencyRunner(t *testing.T) {
	const id = "deps-preflight-test"
	if _, exists := Registry[id]; exists {
		t.Fatalf("test setup: %q already registered", id)
	}
	t.Setenv("PATH", "")
	Register(Language{
		ID:  id,
		Ext: ".py",
		spec: &RunnerSpec{
			ID:         id,
			Deps:       "missing-uv-for-test run {file}",
			DepsMarker: "# /// script",
		},
		Select: func(string) (string, []string, error) {
			t.Fatal("preflight should check uv instead of the ordinary interpreter")
			return "", nil, nil
		},
	})
	defer delete(Registry, id)

	report := Preflight{}.Run([]*LangBlock{{ID: id, Source: "# /// script\nprint(1)"}})
	if report.OK() || len(report.Missing) != 1 {
		t.Fatalf("report = %#v; want one missing dependency runner", report)
	}
	if _, ok := errors.AsType[*NotInstalledError](report.Missing[0].Err); !ok {
		t.Errorf("missing error type = %T; want *NotInstalledError", report.Missing[0].Err)
	}
	if !strings.Contains(report.Error(), "dependency runner") {
		t.Errorf("preflight error does not explain missing tool: %q", report.Error())
	}
}

func TestParseRunnerRequiresCompleteDepsMetadata(t *testing.T) {
	for name, source := range map[string]string{
		"missing marker":            `{"id":"python","ext":".py","find":["python3"],"deps":"uv run {file}"}`,
		"missing file placeholder":  `{"id":"python","ext":".py","find":["python3"],"deps":"uv run","deps_marker":"# /// script"}`,
		"placeholder in executable": `{"id":"python","ext":".py","find":["python3"],"deps":"{file} run","deps_marker":"# /// script"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRunner(source); err == nil {
				t.Fatal("ParseRunner unexpectedly accepted incomplete dependency metadata")
			}
		})
	}
}
