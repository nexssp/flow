package builder

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuild_LooseBundleEmbedded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping standalone build integration test in short mode")
	}

	workDir := t.TempDir()
	bundleDir := filepath.Join(workDir, "mylocal")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	bundleSource := `package mylocal

import (
	"context"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

const ID = "mylocal"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(opts map[string]string) compiler.Bundle {
	prefix := opts["prefix"]
	act := action.New("mylocal.echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": prefix + ":ok"}, nil
	}).Build()
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(filepath.Join(bundleDir, "library.go"), []byte(bundleSource), 0o600); err != nil {
		t.Fatal(err)
	}

	flowPath := filepath.Join(workDir, "test.nflow")
	flowSource := `@require ./mylocal as tools { prefix: "option" }
@assert: result.value == "option:ok"
{} -> tools.echo
`
	if err := os.WriteFile(flowPath, []byte(flowSource), 0o600); err != nil {
		t.Fatal(err)
	}

	binaryName := "built-flow"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(workDir, binaryName)
	if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
		t.Fatalf("nflow build returned %d", code)
	}

	run := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binaryPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	stdout, stderr, err := run()
	if err != nil {
		t.Fatalf("built executable failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"value":"option:ok"`) {
		t.Fatalf("built executable did not run the option-configured aliased bundle; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "nexssflow") {
		t.Fatalf("built executable omitted its run banner; stderr:\n%s", stderr)
	}
	stdout, stderr, err = run("--suppress-output")
	if err != nil || stdout != "" || strings.Contains(stderr, "(built ") {
		t.Fatalf("built executable --suppress-output did not keep stdout clean: err=%v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = run(`--assert=result.value == "not-ok"`)
	if err == nil || !strings.Contains(stderr, `result.value == "not-ok"`) {
		t.Fatalf("built executable did not evaluate the requested failing assertion: err=%v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = run("--info")
	if err != nil || strings.Contains(stdout, `"value":"option:ok"`) || strings.Contains(stderr, "(built ") {
		t.Fatalf("built executable --info did not describe the flow without running it: err=%v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	journalDir := filepath.Join(workDir, "journal")
	stdout, stderr, err = run("--journal-dir", journalDir)
	if err != nil {
		t.Fatalf("built executable journal run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	const executionMarker = "Execution ID: "
	markerAt := strings.Index(stderr, executionMarker)
	if markerAt < 0 {
		t.Fatalf("built executable did not report its execution ID:\n%s", stderr)
	}
	reportedIDs := strings.Fields(stderr[markerAt+len(executionMarker):])
	if len(reportedIDs) == 0 {
		t.Fatalf("built executable reported an empty execution ID:\n%s", stderr)
	}
	executionID := reportedIDs[0]
	stdout, stderr, err = run("--journal-dir="+journalDir, "--execution-id", executionID)
	if err != nil {
		t.Fatalf("built executable replay failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, executionMarker+executionID) || !strings.Contains(stdout, `"value":"option:ok"`) {
		t.Fatalf("built executable replay output/identity mismatch:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	stdout, stderr, err = run("--json")
	if err != nil {
		t.Fatalf("built executable --json failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "(built ") {
		t.Fatalf("--json unexpectedly printed the embedded version banner; stderr:\n%s", stderr)
	}

	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"-vvvv"}} {
		stdout, stderr, err = run(args...)
		if err != nil || !strings.Contains(stdout, `"value":"option:ok"`) || strings.Contains(stdout, "Nexss Flow standalone executable.") {
			t.Fatalf("standalone did not forward %q to the embedded workflow: err=%v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
		}
	}

	stdout, stderr, err = run("version")
	if err != nil {
		t.Fatalf("built executable version failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout == "" || strings.Contains(stderr, "(built ") {
		t.Fatalf("embedded version output changed; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	stdout, stderr, err = run("info")
	if err != nil {
		t.Fatalf("built executable info failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout == "" && stderr == "" {
		t.Fatal("embedded info produced no output")
	}
	if strings.Contains(stderr, "(built ") {
		t.Fatalf("embedded info unexpectedly printed the version banner; stderr:\n%s", stderr)
	}
}

func TestStageBuildDir_ResilienceWithRetryMountsOnePolicyBundle(t *testing.T) {
	workDir := t.TempDir()
	flowPath := filepath.Join(workDir, "resilience.nflow")
	childPath := filepath.Join(workDir, "child.nflow")
	if err := os.WriteFile(childPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	source := "@require resilience\n@include \"" + childPath + "\"\npayment.charge :retry\n"
	if err := os.WriteFile(flowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	buildDir, err := stageBuildDir(t.Context(), flowPath, []byte(source), nil, buildOptions{})
	if err != nil {
		t.Fatalf("stage generated binary: %v", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	generated, err := os.ReadFile(filepath.Join(buildDir, "main.go"))
	if err != nil {
		t.Fatalf("read generated main: %v", err)
	}
	mainSource := string(generated)
	if !strings.Contains(mainSource, `const sourceName = "resilience.nflow"`) || strings.Contains(mainSource, workDir) {
		t.Fatalf("generated main embedded an absolute source path instead of a stable relative name:\n%s", mainSource)
	}
	manifest, err := os.ReadFile(filepath.Join(buildDir, "sources.json"))
	if err != nil {
		t.Fatalf("read generated source manifest: %v", err)
	}
	if strings.Contains(string(manifest), workDir) {
		t.Fatalf("generated source manifest contains an absolute temp path: %s", manifest)
	}
	var sourceFiles map[string]string
	if err := json.Unmarshal(manifest, &sourceFiles); err != nil {
		t.Fatalf("decode generated source manifest: %v", err)
	}
	workflow, err := os.ReadFile(filepath.Join(buildDir, "workflow.nflow"))
	if err != nil {
		t.Fatalf("read embedded workflow: %v", err)
	}
	if strings.Contains(string(workflow), workDir) || !strings.Contains(string(workflow), `@include "child.nflow"`) {
		t.Fatalf("embedded root source is not path-normalized: %s", workflow)
	}
	if got := string(workflow); got != sourceFiles["resilience.nflow"] {
		t.Fatalf("embedded workflow and source manifest root differ: embedded=%q manifest=%q", got, sourceFiles["resilience.nflow"])
	}
	if !strings.Contains(mainSource, `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`) ||
		!strings.Contains(mainSource, `ctx = compiler.WithSourceLoader(ctx,sourceFiles)`) {
		t.Fatalf("generated main does not use a signal-aware source-loader context:\n%s", mainSource)
	}
	runAt := strings.Index(mainSource, "exitCode := runner.RunSource")
	stopAt := strings.Index(mainSource, "stop()")
	shutdownAt := strings.LastIndex(mainSource, "if shutdownErr := closeHost();")
	if runAt < 0 || stopAt < runAt || shutdownAt < stopAt {
		t.Fatalf("signal notifications are not stopped after execution and before host shutdown:\n%s", mainSource)
	}
	if strings.Contains(mainSource, `"github.com/nexssp/flow/extensions/retry"`) {
		t.Fatalf("generated binary imported the legacy retry adviser:\n%s", mainSource)
	}

	var resilienceAlias string
	for line := range strings.SplitSeq(mainSource, "\n") {
		if strings.Contains(line, `"github.com/nexssp/flow/extensions/resilience"`) {
			resilienceAlias = strings.Fields(line)[0]
			break
		}
	}
	if resilienceAlias == "" {
		t.Fatalf("generated binary did not import the resilience bundle:\n%s", mainSource)
	}
	if got := strings.Count(mainSource, resilienceAlias+".Bundle(nil)"); got != 1 {
		t.Fatalf("generated binary mounts resilience %d times, want exactly one policy path", got)
	}
}

func TestBuild_LocalNestedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping standalone local-package build integration test in short mode")
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.invalid/localrepo\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(workDir, "sub", "worker")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundleSource := `package worker

import (
	"context"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

const ID = "worker"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(opts map[string]string) compiler.Bundle {
	prefix := opts["prefix"]
	act := action.New("worker.echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": prefix + ":ok"}, nil
	}).Build()
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{act}}},
	}
}
`
	if err := os.WriteFile(filepath.Join(bundleDir, "library.go"), []byte(bundleSource), 0o600); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(workDir, "local.nflow")
	flow := `@require ./sub/worker as local { prefix: "nested" }
@assert: result.value == "nested:ok"
{} -> local.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join(workDir, "local-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
		t.Fatalf("nflow build with a local nested package returned %d", code)
	}
	cmd := exec.CommandContext(t.Context(), binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built local flow failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"value":"nested:ok"`) {
		t.Fatalf("built flow did not preserve aliased option-configured local bundle output:\n%s", output)
	}
}
