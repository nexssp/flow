package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverTestFlowsOneLevelAndSorted(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "scenario-z", "second.nflow"), "const @{ value: \"z\" }\n")
	writeTestFlow(t, filepath.Join(root, "scenario-a", "main.nflow"), "const @{ value: \"a\" }\n")
	writeTestFlow(t, filepath.Join(root, "scenario-a", "nested", "helper.nflow"), "this is not a test entrypoint\n")
	writeTestFlow(t, filepath.Join(root, "root.nflow"), "this is not a test entrypoint\n")

	got, err := discoverTestFlows(root)
	if err != nil {
		t.Fatalf("discoverTestFlows() error = %v", err)
	}
	want := []string{
		filepath.Join(root, "scenario-a", "main.nflow"),
		filepath.Join(root, "scenario-z", "second.nflow"),
	}
	if len(got) != len(want) {
		t.Fatalf("discoverTestFlows() returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discoverTestFlows()[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTestCommandRunsEveryFlowAndEvaluatesAssertions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "spec")
	bad := filepath.Join(root, "01_bad", "main.nflow")
	good := filepath.Join(root, "02_good", "main.nflow")
	ignored := filepath.Join(root, "02_good", "common", "helper.nflow")
	writeTestFlow(t, good, "const @{ value: \"good\" }\n@assert: result == \"good\"\n")
	writeTestFlow(t, bad, "const @{ value: \"bad\" }\n@assert: result == \"good\"\n")
	writeTestFlow(t, ignored, "not valid Flow source\n")

	code, stdout, stderr := runTestCommand(t, []string{"test", root})
	if code == 0 {
		t.Fatalf("nflow test exit code = 0, want assertion failure\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "PASS: "+good) {
		t.Errorf("stdout missing pass for %q:\n%s", good, stdout)
	}
	if !strings.Contains(stderr, "FAIL: "+bad) {
		t.Errorf("stderr missing failure for %q:\n%s", bad, stderr)
	}
	if strings.Contains(stdout+stderr, ignored) {
		t.Errorf("nested helper was incorrectly treated as an entrypoint:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Index(stdout, "=== "+bad+" ===") > strings.Index(stdout, "=== "+good+" ===") {
		t.Errorf("flows were not run in sorted order:\n%s", stdout)
	}
}

func TestTestCommandMountsTestExtensionOnlyInTestMode(t *testing.T) {
	root := filepath.Join("..", "extensions", "test", "testdata", "spec")
	code, stdout, stderr := runTestCommand(t, []string{"test", root})
	if code == 0 {
		t.Fatalf("nflow test exit code = 0, want the deliberate @test failure\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	pass := filepath.Join(root, "01_pass", "main.nflow")
	fail := filepath.Join(root, "02_fail", "main.nflow")
	if !strings.Contains(stdout, "PASS: "+pass) {
		t.Errorf("stdout missing passing @test fixture %q:\n%s", pass, stdout)
	}
	if !strings.Contains(stderr, "FAIL: "+fail) || !strings.Contains(stderr, `result == "ok"`) {
		t.Errorf("stderr missing failing @test fixture and assertion:\n%s", stderr)
	}

	code, _, stderr = runTestCommand(t, []string{"run", pass})
	if code == 0 || !strings.Contains(stderr, "@test") {
		t.Fatalf("ordinary nflow run accepted test-only @test: code=%d stderr=%q", code, stderr)
	}
}

func TestTestCommandRejectsInvalidInvocation(t *testing.T) {
	root := t.TempDir()

	code, _, stderr := runTestCommand(t, []string{"test", root})
	if code == 0 || !strings.Contains(stderr, "no .nflow entrypoints") {
		t.Fatalf("empty test directory: code=%d stderr=%q; want non-zero and no-entrypoint diagnostic", code, stderr)
	}

	code, _, stderr = runTestCommand(t, []string{"test", root, "extra"})
	if code == 0 || !strings.Contains(stderr, "usage: nflow test [directory]") {
		t.Fatalf("extra argument: code=%d stderr=%q; want usage error", code, stderr)
	}
}

func TestTestCommandIsNotTreatedAsToolchainFlow(t *testing.T) {
	flow := filepath.Join(t.TempDir(), "main.nflow")
	writeTestFlow(t, flow, "const @{ value: \"ok\" }\n")
	if isFileBasedRuntimeInvocation([]string{"test", flow}) {
		t.Fatal("nflow test was classified as a file-based run invocation")
	}
}

func TestTestCommandHelp(t *testing.T) {
	var output strings.Builder
	if !PrintCommandHelp(&output, "test") {
		t.Fatal("PrintCommandHelp(test) returned false")
	}
	for _, want := range []string{"nflow test [directory]", "nflow test ./spec"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("test help missing %q:\n%s", want, output.String())
		}
	}
}

func writeTestFlow(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create flow directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write test flow: %v", err)
	}
}

func runTestCommand(t *testing.T, args []string) (exitCode int, stdout, stderr string) {
	t.Helper()
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		t.Fatalf("create CLI invocation: %v", err)
	}

	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	originalStdout, originalStderr := os.Stdout, os.Stderr
	originalLogger := slog.Default()
	os.Stdout, os.Stderr = stdoutWrite, stderrWrite
	stdoutDone := make(chan string, 1)
	stderrDone := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stdoutRead)
		stdoutDone <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(stderrRead)
		stderrDone <- string(data)
	}()

	exitCode = runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runCLI(ctx, inv, args)
	})
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	os.Stdout, os.Stderr = originalStdout, originalStderr
	slog.SetDefault(originalLogger)

	stdout = <-stdoutDone
	stderr = <-stderrDone
	_ = stdoutRead.Close()
	_ = stderrRead.Close()
	return exitCode, stdout, stderr
}
