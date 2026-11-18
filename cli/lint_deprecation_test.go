package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestLintRejectsLegacyArgumentBlocksWithReplacementHints(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   string
	}{
		{
			source: `wrap @{ key: "greeting" }`,
			want:   "legacy `@{...}` arguments are not supported; replace them with `wrap(key: value)`",
		},
		{
			source: `const @{ value: "Julia" }`,
			want:   "legacy `const @{...}` arguments are not supported; use `const <value>` for a scalar or `const(key: value)` to construct an object",
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			issues := lintOne(t, tc.source)
			ktest.RequireLen(t, issues, 1)
			ktest.RequireEqual(t, issues[0].File, "test.nflow")
			ktest.RequireEqual(t, issues[0].Kind, lintIssueCompile)
			ktest.RequireStringContains(t, issues[0].Message, tc.want)
		})
	}
}

func TestLintWarnsOnLegacyConstKeyValueMagicParameters(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		wantMessage string
	}{
		{
			name:        "value unwrap",
			source:      `const(value=42)`,
			wantMessage: "legacy const key=value scalar wrapper using `value`",
		},
		{
			name:        "val unwrap with coerce",
			source:      `noop -> const(val=42, coerce=true)`,
			wantMessage: "literal `coerce=true` is text, not a boolean",
		},
		{
			name:        "value takes precedence",
			source:      `const(value=1, val=2)`,
			wantMessage: "`value` takes precedence over `val`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := lintOne(t, tc.source)
			ktest.RequireLen(t, issues, 1)
			issue := issues[0]
			ktest.RequireEqual(t, issue.File, "test.nflow")
			ktest.RequireEqual(t, issue.Line, 1)
			ktest.RequireEqual(t, issue.Column, strings.Index(tc.source, "const")+1)
			ktest.RequireEqual(t, issue.Kind, lintIssueWarning)
			ktest.RequireStringContains(t, issue.Message, "hint:")
			ktest.RequireStringContains(t, issue.Message, tc.wantMessage)
		})
	}
}

func TestLintDoesNotWarnOnParenthesizedOrKeyEqualsCallsOrExpressionPipes(t *testing.T) {
	for _, source := range []string{
		`runtime.noop(key: 1)`,
		`runtime.noop(key=1)`,
		`runtime.noop(value=1, val=2, coerce=true)`,
		`const(count=1)`,
		`const(coerce=true)`,
		`const(value: 1, val: 2, coerce: true)`,
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
