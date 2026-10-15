package builder

import (
	"bytes"
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

	stdout, stderr, err = run("help")
	if err != nil {
		t.Fatalf("built executable help failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Usage:") ||
		!strings.Contains(stdout, "--journal-dir DIR") ||
		!strings.Contains(stdout, "--execution-id ID") ||
		strings.Contains(stderr, "(built ") {
		t.Fatalf("embedded help output changed; stdout:\n%s\nstderr:\n%s", stdout, stderr)
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
	source := "@require resilience\npayment.charge :retry\n"
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
