package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestLintRejectsLegacyArgumentBlocksWithReplacementHints(t *testing.T) {
	const source = `wrap @{ key: "greeting" }`
	issues := lintOne(t, source)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].File, "test.nflow")
	ktest.RequireEqual(t, issues[0].Kind, lintIssueCompile)
	ktest.RequireStringContains(t, issues[0].Message, "legacy `@{...}` arguments are not supported; replace them with `wrap(key: value)`")
}

func TestLintAcceptsCurrentArgumentAndValueSyntax(t *testing.T) {
	for _, source := range []string{
		`runtime.noop(key: 1)`,
		`runtime.noop(key=1)`,
		`runtime.noop(value=1, val=2, coerce=true)`,
		`{ count: 1, label: "ready" }`,
		`{ items: [{ line: 1, severity: "low" }, { line: 2, severity: "high" }] } -> { top: items | filter(#.severity == "high") | sortBy(#.line, "desc") | take(1) }`,
	} {
		if issues := lintOne(t, source); len(issues) != 0 {
			t.Errorf("lint(%q) = %#v, want no warnings or errors", source, issues)
		}
	}
}

func TestLintCommandReportsLegacyArgumentCompileError(t *testing.T) {
	const source = `runtime.noop @{ key: "x" }`
	path := filepath.Join(t.TempDir(), "legacy.nflow")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := captureLintOutput(t, func() int {
		return runLint([]string{path})
	})
	ktest.RequireEqual(t, exitCode, 1)
	ktest.RequireEqual(t, stderr, "")

	var issues []LintIssue
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		t.Fatalf("decode lint issues: %v\nstdout: %s", err, stdout)
	}
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, lintIssueCompile)
	ktest.RequireStringContains(t, issues[0].Message, "replace them with `noop(key: value)`")
}
