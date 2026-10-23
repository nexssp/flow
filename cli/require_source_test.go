package cli

import (
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
