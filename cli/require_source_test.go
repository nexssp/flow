package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestRequireSourceArguments(t *testing.T) {
	for _, args := range [][]string{
		{"flow.nflow", "--package", "example.com/acme/*"},
		{"--package=example.com/acme/*", "flow.nflow"},
		{"--package", "example.com/acme/*", "flow.nflow"},
	} {
		file, pattern, help, err := parseRequireSourceArgs(args, true)
		ktest.RequireNoError(t, err)
		ktest.RequireEqual(t, file, "flow.nflow")
		ktest.RequireEqual(t, pattern, "example.com/acme/*")
		ktest.RequireFalse(t, help)
	}
	for _, args := range [][]string{nil, {"one", "two"}, {"one", "--package"}, {"one", "--package="}, {"one", "--unknown"}, {"one", "--package", "a", "--package", "b"}} {
		_, _, _, err := parseRequireSourceArgs(args, true)
		ktest.RequireCondition(t, err != nil, "invalid arguments must fail: %v", args)
	}
	_, _, _, err := parseRequireSourceArgs([]string{"one", "--package", "a"}, false)
	ktest.RequireCondition(t, err != nil, "reset must reject package filtering")
}

func TestRequireSourceCommandHelpAndUsage(t *testing.T) {
	for _, operation := range []string{"check", "update", "pin", "reset"} {
		t.Run(operation, func(t *testing.T) {
			var code int
			stdout, _ := captureIO(t, func() { code = RunContext(t.Context(), []string{"require", operation, "--help"}) })
			ktest.RequireEqual(t, code, 0)
			ktest.RequireStringContains(t, stdout, "nflow require "+operation)
			_, stderr := captureIO(t, func() { code = RunContext(t.Context(), []string{"require", operation}) })
			ktest.RequireEqual(t, code, 2)
			ktest.RequireStringContains(t, stderr, "usage:")
		})
	}
	var code int
	_, stderr := captureIO(t, func() { code = RunContext(t.Context(), []string{"require", "freeze", "flow.nflow"}) })
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "freeze was removed")
}

func TestRequireSourceBuiltinsIgnoreSidecars(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flow.nflow")
	original := "@require runtime\nruntime.noop\n"
	ktest.RequireNoError(t, os.WriteFile(file, []byte(original), 0o600))
	ktest.RequireNoError(t, os.WriteFile(file+".lock", []byte("malformed obsolete sidecar"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(file+".bak", []byte("prior source"), 0o600))
	for _, args := range [][]string{{"require", "check", file}, {"require", "update", file}, {"require", "pin", file}, {"pin", file}} {
		var code int
		stdout, _ := captureIO(t, func() { code = RunContext(t.Context(), args) })
		ktest.RequireEqual(t, code, 0)
		ktest.RequireStringContains(t, stdout, "skipped")
	}
	data, err := os.ReadFile(file)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(data), original)
	data, err = os.ReadFile(file + ".bak")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(data), "prior source")
	var code int
	_, _ = captureIO(t, func() { code = RunContext(t.Context(), []string{"require", "reset", file}) })
	ktest.RequireEqual(t, code, 0)
	data, err = os.ReadFile(file)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, string(data), "prior source")
	_, err = os.Stat(file + ".bak")
	ktest.RequireTrue(t, os.IsNotExist(err))
}

func TestRequireSourceExecutionIgnoresSidecars(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "flow.nflow")
	other := filepath.Join(root, "other.nflow")
	const source = "runtime.noop\n"
	for _, path := range []string{file, other} {
		ktest.RequireNoError(t, os.WriteFile(path, []byte(source), 0o600))
		ktest.RequireNoError(t, os.WriteFile(path+".bak", []byte("obsolete backup"), 0o600))
	}
	for _, sidecar := range []string{"missing", "malformed"} {
		t.Run(sidecar, func(t *testing.T) {
			if sidecar == "malformed" {
				for _, path := range []string{file, other} {
					ktest.RequireNoError(t, os.WriteFile(path+".lock", []byte("obsolete lock"), 0o600))
				}
			}
			for _, args := range [][]string{
				{"run", file, "--json"},
				{file, "--json"},
				{"lint", file},
				{"lint", file, other},
				{"lint", root},
				{"lint", filepath.Join(root, "...")},
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					withStdin(t, "")
					var code int
					_, stderr := captureIO(t, func() { code = RunContext(t.Context(), args) })
					ktest.RequireCondition(t, code == 0, "CLI failed: %s", stderr)
				})
			}
			t.Run("RunPath", func(t *testing.T) {
				withStdin(t, "")
				var code int
				_, stderr := captureIO(t, func() { code = RunPath(t.Context(), []string{file, "--json"}) })
				ktest.RequireCondition(t, code == 0, "RunPath failed: %s", stderr)
			})
		})
	}
	for _, path := range []string{file, other} {
		for suffix, want := range map[string]string{"": source, ".bak": "obsolete backup", ".lock": "obsolete lock"} {
			data, err := os.ReadFile(path + suffix)
			ktest.RequireNoError(t, err)
			ktest.RequireEqual(t, string(data), want)
		}
	}
}

func TestRequireSourceBatchLintIgnoresSidecarsWithExternalRequirements(t *testing.T) {
	// Exercise batch dispatch without downloading an external module. An existing
	// harness must report the missing bundle, not reject obsolete sidecars.
	t.Setenv(harnessEnv, "1")
	root := t.TempDir()
	good := filepath.Join(root, "good.nflow")
	external := filepath.Join(root, "external.nflow")
	ktest.RequireNoError(t, os.WriteFile(good, []byte("runtime.noop\n"), 0o600))
	ktest.RequireNoError(t, os.WriteFile(external, []byte("@require example.invalid/module/nexssflow v0.0.1\nmodule.action\n"), 0o600))
	for _, path := range []string{good, external} {
		ktest.RequireNoError(t, os.WriteFile(path+".lock", []byte("obsolete lock"), 0o600))
	}
	var code int
	stdout, _ := captureIO(t, func() { code = RunContext(t.Context(), []string{"lint", root}) })
	ktest.RequireEqual(t, code, 1)
	var issues []LintIssue
	ktest.RequireNoError(t, json.Unmarshal([]byte(stdout), &issues))
	ktest.RequireEqual(t, len(issues), 1)
	ktest.RequireEqual(t, issues[0].File, external)
	ktest.RequireEqual(t, issues[0].Kind, "config")
}

func TestRequireSourceHelpAndCompletion(t *testing.T) {
	var help strings.Builder
	ktest.RequireTrue(t, PrintCommandHelp(&help, "require"))
	for _, operation := range []string{"check", "update", "pin", "reset"} {
		ktest.RequireStringContains(t, help.String(), operation)
	}
	ktest.RequireCondition(t, !strings.Contains(help.String(), "require freeze"), "help must not advertise removed freeze")
	for _, shell := range []string{"bash", "zsh", "pwsh", "powershell"} {
		var script strings.Builder
		ktest.RequireNoError(t, Completion(shell, &script))
		for _, operation := range []string{"check", "update", "pin", "reset"} {
			ktest.RequireStringContains(t, script.String(), operation)
		}
		ktest.RequireCondition(t, !strings.Contains(script.String(), "freeze"), "completion must not advertise removed freeze")
	}
}
