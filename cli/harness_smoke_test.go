package cli_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmoke_ForkExtension proves the ./nexssflow contract end-to-end:
// a repository that has never seen Nexss becomes a Flow extension by
// adding a nexssflow/ subdirectory with a Bundle function.
//
// The test builds a real external module in a temp directory, invokes
// `nflow run` through the harness, and checks the action's output.
//
// Skipped in -short mode. Enable with: go test ./cli/... -run TestSmoke
func TestSmoke_ForkExtension(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("smoke test; skipped in short mode")
	}
	// Isolate harness cache: this test never touches the developer's
	// real cache, and a stale entry from a previous run can never
	// leak into the test.
	isolateHarnessCache(t)
	goBin := requireTool(t, "go")
	flowRoot := flowModuleRoot(t)
	kernelRoot := kernelModuleRoot(t, flowRoot)

	workDir := t.TempDir()
	writeForkModule(t, workDir, flowRoot, kernelRoot)
	writeFile(t, filepath.Join(workDir, "test.nflow"), `@description "fork smoke"
@require ./mylib
@assert: result == "hello from fork"
mylib.hello
`)

	output := runNflow(t, goBin, flowRoot, "run", filepath.Join(workDir, "test.nflow"))
	if !strings.Contains(output, "hello from fork") {
		t.Fatalf("expected greeting in output; got:\n%s", output)
	}
}

// TestSmoke_PolyglotExtension proves a Python repository can be used
// from Flow without rewriting its logic in Go. The nexssflow shim runs
// python3 as a subprocess and decodes its JSON stdout — the same
// pattern Nexss Programmer 2.x used to chain runtimes.
//
// Skipped in -short mode. Skipped when no working python interpreter is
// present, because a missing interpreter is not a Nexss bug.
func TestSmoke_PolyglotExtension(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("smoke test; skipped in short mode")
	}
	goBin := requireTool(t, "go")

	// The shim prefers python3 then python; the test needs at least one
	// of them working. Windows Store shims resolve in PATH but fail
	// with exit 9009 when executed, so requireTool probes with --version.
	pythonBin := firstWorkingTool(t, "python3", "python")
	if pythonBin == "" {
		t.Skip("no working python interpreter; polyglot smoke test skipped")
	}

	flowRoot := flowModuleRoot(t)
	kernelRoot := kernelModuleRoot(t, flowRoot)

	workDir := t.TempDir()
	writePolyglotModule(t, workDir, flowRoot, kernelRoot)
	writeFile(t, filepath.Join(workDir, "test.nflow"), `@description "polyglot smoke"
@require ./pyrepo
@assert: result.py == "hello from python"
py.echo
`)

	output := runNflow(t, goBin, flowRoot, "run", filepath.Join(workDir, "test.nflow"))
	if !strings.Contains(output, `"py":"hello from python"`) &&
		!strings.Contains(output, `"py": "hello from python"`) {
		t.Fatalf("expected python greeting in output; got:\n%s", output)
	}
}

// ── module fixtures ─────────────────────────────────────────────────

func writeForkModule(t *testing.T, workDir, flowRoot, kernelRoot string) {
	t.Helper()
	myLib := filepath.Join(workDir, "mylib")
	mustMkdir(t, filepath.Join(myLib, "nexssflow"))

	writeFile(t, filepath.Join(myLib, "go.mod"), moduleGoMod("example.com/mylib", flowRoot, kernelRoot))

	writeFile(t, filepath.Join(myLib, "nexssflow", "library.go"), `// Package nexssflow is the Flow entry point for this repository.
//
// The Bundle function is the only symbol the harness needs. Its ID is
// what @require resolves against; its library name is what the
// resolver mounts. The action mylib.hello is what the .nflow file
// invokes.
package nexssflow

import (
	"context"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "mylib"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{hello()},
		}},
	}
}

func hello() action.AnyAction {
	return action.New("mylib.hello", func(_ context.Context, _ any) (string, error) {
		return "hello from fork", nil
	}).Build()
}
`)
}

func writePolyglotModule(t *testing.T, workDir, flowRoot, kernelRoot string) {
	t.Helper()
	pyRepo := filepath.Join(workDir, "pyrepo")
	mustMkdir(t, filepath.Join(pyRepo, "nexssflow"))

	writeFile(t, filepath.Join(pyRepo, "go.mod"), moduleGoMod("example.com/pyrepo", flowRoot, kernelRoot))

	writeFile(t, filepath.Join(pyRepo, "nexssflow", "library.go"), `// Package nexssflow exposes a Python script to Flow through a thin Go
// shim. The repository itself is Python; nothing here duplicates the
// script's logic — the shim runs it and decodes JSON stdout.
package nexssflow

import (
	"context"
	"encoding/json"
	"os/exec"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

const ID = "pyrepo"

// pythonScript is the payload the shim executes. In a real repository
// this would be a file next to the shim; inlined here to keep the
// smoke test self-contained.
const pythonScript = `+"`"+`import json; print(json.dumps({"py": "hello from python"}))`+"`"+`

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{pyEcho()},
		}},
	}
}

// pyEcho runs the Python script and decodes its JSON stdout. The
// interpreter is discovered at call time; a missing python3 falls back
// to python (Windows).
func pyEcho() action.AnyAction {
	return action.New("py.echo", func(ctx context.Context, _ any) (any, error) {
		python, err := findPython()
		if err != nil {
			return nil, xerr.Unavailable("pyrepo: " + err.Error())
		}
		cmd := exec.CommandContext(ctx, python, "-c", pythonScript)
		raw, err := cmd.Output()
		if err != nil {
			return nil, xerr.Internal("pyrepo: python failed", err)
		}
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, xerr.Internal("pyrepo: invalid JSON from python", err)
		}
		return parsed, nil
	}).Description("Run the Python echo script").
		Tag("polyglot", "python").
		Build()
}

// findPython returns the first interpreter in PATH that both resolves
// and executes. A Windows Store shim lives in PATH but fails with exit
// 9009 when run, so LookPath alone is not enough.
func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--version").Run(); err != nil {
			continue
		}
		return path, nil
	}
	return "", xerr.NotFound("no working python interpreter in PATH")
}
`)
}

// moduleGoMod builds a minimal go.mod that pins the local flow and
// kernel modules through replace directives. This mirrors what a real
// downstream repository does during development, before publishing
// versioned tags.
func moduleGoMod(modulePath, flowRoot, kernelRoot string) string {
	return "module " + modulePath + "\n\ngo 1.26\n\n" +
		"require (\n" +
		"\tgithub.com/nexssp/flow v0.0.0\n" +
		"\tgithub.com/nexssp/kernel v0.0.0\n" +
		")\n\n" +
		"replace github.com/nexssp/flow => " + flowRoot + "\n" +
		"replace github.com/nexssp/kernel => " + kernelRoot + "\n"
}

// ── harness invocation ─────────────────────────────────────────────

// runNflow runs `go run ./cmd/nflow ARGS...` inside flowRoot and
// returns the combined stdout+stderr. Failing invocations print both
// streams in the test failure message.
func runNflow(t *testing.T, goBin, flowRoot string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), goBin, append([]string{"run", "./cmd/nflow"}, args...)...) //nolint:gosec // G204: args are constructed by the test, not user input
	cmd.Dir = flowRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("nflow %v failed: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
			args, err, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}

// ── helpers ────────────────────────────────────────────────────────

// requireTool returns the path to a tool that both resolves in PATH and
// actually executes. A Windows Store shim resolves but fails on run,
// so the version probe keeps the caller's assertion honest.
func requireTool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not found in PATH: %v", name, err)
	}
	if !toolExecutes(path, name) {
		t.Skipf("%s found at %s but does not execute", name, path)
	}
	return path
}

// firstWorkingTool returns the first name in PATH that both resolves
// and executes. Used where two interpreters are equally acceptable and
// a broken one must not fail the test.
func firstWorkingTool(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if toolExecutes(path, name) {
			return path
		}
	}
	return ""
}

// toolExecutes probes a tool with its native version flag. Getting this
// wrong is what turned a green smoke test into a skip: `go --version`
// exits 2, while `go version` succeeds.
func toolExecutes(path, name string) bool {
	flag := "--version"
	if strings.HasPrefix(name, "go") {
		flag = "version"
	}
	return exec.CommandContext(context.Background(), path, flag).Run() == nil
}

// isolateHarnessCache points NFLOW_HARNESS_CACHE at a fresh temp dir
// for the duration of the test. It is intentionally not t.Setenv:
// t.Setenv conflicts with t.Parallel because it mutates process state.
// Two smoke tests running in parallel would race on the same env var;
// we accept that because each smoke test is slow enough that they
// rarely overlap, and a shared temp dir is still better than the
// shared user cache.
func isolateHarnessCache(t *testing.T) {
	t.Helper()
	previous, hadPrevious := os.LookupEnv("NFLOW_HARNESS_CACHE")
	dir := t.TempDir()

	//nolint:usetesting // t.Setenv is unavailable after t.Parallel
	if err := os.Setenv("NFLOW_HARNESS_CACHE", dir); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		//nolint:usetesting // restoring process env, not a t.Setenv scope
		if hadPrevious {
			_ = os.Setenv("NFLOW_HARNESS_CACHE", previous)
		} else {
			_ = os.Unsetenv("NFLOW_HARNESS_CACHE")
		}
	})
}

func flowModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/nexssp/flow") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find flow module root from " + dir)
		}
		dir = parent
	}
}

func kernelModuleRoot(t *testing.T, flowRoot string) string {
	t.Helper()
	candidate := filepath.Join(filepath.Dir(flowRoot), "kernel")
	if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err != nil {
		t.Skipf("kernel module not found at %s: %v", candidate, err)
	}
	return candidate
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
