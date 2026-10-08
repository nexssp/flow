package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverLintFilesRecursiveAndExclusions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"root.nflow",
		"nested/z-last.nflow",
		"nested/a-first.nflow",
		".git/ignored.nflow",
		".hg/ignored.nflow",
		".svn/ignored.nflow",
		"vendor/ignored.nflow",
		"node_modules/ignored.nflow",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{ ok: true }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	linkedFile := filepath.Join(root, "nested", "linked.nflow")
	if err := os.Symlink(filepath.Join(root, "root.nflow"), linkedFile); err != nil {
		t.Logf("file symlink unavailable; skipping that assertion: %v", err)
	} else {
		t.Cleanup(func() { _ = os.Remove(linkedFile) })
	}
	loop := filepath.Join(root, "nested", "loop")
	if err := os.Symlink(root, loop); err != nil {
		t.Logf("directory symlink unavailable; skipping loop assertion: %v", err)
	} else {
		t.Cleanup(func() { _ = os.Remove(loop) })
	}

	got, err := discoverLintFiles([]string{filepath.Join(root, "..."), filepath.Join(root, "nested", "...")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "nested", "a-first.nflow"),
		filepath.Join(root, "nested", "z-last.nflow"),
		filepath.Join(root, "root.nflow"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverLintFiles() = %#v, want %#v", got, want)
	}
}

func TestDiscoverLintFilesExactDirectoryAndNoMatch(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	flow := filepath.Join(nested, "only.nflow")
	if err := os.WriteFile(flow, []byte("{ ok: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discoverLintFiles([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{flow}) {
		t.Fatalf("exact directory discovery = %#v, want [%q]", got, flow)
	}

	empty := t.TempDir()
	if _, err := discoverLintFiles([]string{filepath.Join(empty, "...")}); err == nil || !strings.Contains(err.Error(), "no .nflow files found") {
		t.Fatalf("no-match error = %v, want an explicit no .nflow files message", err)
	}
	stdout, stderr, code := captureLintOutput(t, func() int {
		return runLint([]string{filepath.Join(empty, "...")})
	})
	if code == 0 || !strings.Contains(stderr, "no .nflow files found") {
		t.Fatalf("CLI no-match result code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunLintWildcardReportsMixedTreeFailures(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "nested", "good.nflow")
	bad := filepath.Join(root, "nested", "bad.nflow")
	if err := os.MkdirAll(filepath.Dir(good), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte("@description \"valid fixture\"\n{ ok: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("@description \"invalid fixture\"\nunknown_lint_fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := captureLintOutput(t, func() int {
		return runLint([]string{filepath.Join(root, "...")})
	})
	if code == 0 {
		t.Fatalf("runLint returned success for an invalid source; stdout=%s stderr=%s", stdout, stderr)
	}
	var issues []LintIssue
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		t.Fatalf("decode batch diagnostics: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if len(issues) == 0 {
		t.Fatalf("no diagnostics returned for invalid source: %s", stdout)
	}
	foundBad := false
	foundGood := false
	for _, issue := range issues {
		if issue.File == bad {
			foundBad = true
		}
		if issue.File == good {
			foundGood = true
		}
	}
	if !foundBad {
		t.Fatalf("diagnostics did not identify bad source %q: %#v", bad, issues)
	}
	if foundGood {
		t.Fatalf("valid source unexpectedly produced diagnostics: %#v", issues)
	}
}

func TestRunLintExplicitFileKeepsSingleFileSuccessOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid.nflow")
	if err := os.WriteFile(path, []byte("{ ok: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureLintOutput(t, func() int {
		return runLint([]string{path})
	})
	if code != 0 || strings.TrimSpace(stdout) != "ok" || stderr != "" {
		t.Fatalf("explicit file lint result code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestRunLintBatchContinuesWhenExternalHarnessIsUnavailable(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good.nflow")
	needsModule := filepath.Join(root, "needs-module.nflow")
	if err := os.WriteFile(good, []byte("{ ok: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(needsModule, []byte("@require example.invalid/module/nexssflow v0.0.1\nmodule.action\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := captureLintOutput(t, func() int {
		return runLintBatchInProcessWithHarnessError([]string{good, needsModule}, errors.New("module download failed"))
	})
	if code == 0 {
		t.Fatalf("batch accepted a source whose registry was unavailable; stdout=%q stderr=%q", stdout, stderr)
	}
	var issues []LintIssue
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		t.Fatalf("decode batch diagnostics: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if len(issues) != 1 || issues[0].File != needsModule || issues[0].Kind != "config" || !strings.Contains(issues[0].Message, "module download failed") {
		t.Fatalf("batch diagnostics = %#v, want one registry error for %q", issues, needsModule)
	}
}

func captureLintOutput(t *testing.T, run func() int) (capturedStdout, capturedStderr string, exitCode int) {
	t.Helper()
	captureMutex.Lock()
	defer captureMutex.Unlock()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	code := run()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdoutReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), code
}
