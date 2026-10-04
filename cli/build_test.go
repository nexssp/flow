package cli

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

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "mylocal"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	prefix := opts["prefix"]
	act := action.New("mylocal.echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": prefix + ":ok"}, nil
	}).Build()
	return core.Bundle{
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
	if !strings.Contains(stdout, "Usage:") || strings.Contains(stderr, "(built ") {
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

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "worker"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	prefix := opts["prefix"]
	act := action.New("worker.echo", func(_ context.Context, _ any) (map[string]any, error) {
		return map[string]any{"value": prefix + ":ok"}, nil
	}).Build()
	return core.Bundle{
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
