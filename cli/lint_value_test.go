package cli

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func lintOne(t *testing.T, src string) []LintIssue {
	t.Helper()
	cfg, err := buildConfig(nil)
	ktest.RequireNoError(t, err)
	return lintFile("test.nflow", src, cfg)
}

func TestLintCompileValidValues(t *testing.T) {
	for _, src := range []string{
		`runtime.noop:timeout=5s`,
		`runtime.noop:retry=3`,
		`runtime.noop:dedup`,
		`runtime.noop:role=admin,editor`,
	} {
		ktest.RequireLen(t, lintOne(t, src), 0)
	}
}

func TestLintCompileRejectsInvalidModifierValues(t *testing.T) {
	for _, tc := range []struct {
		source  string
		message string
	}{
		{`runtime.noop:timeout=notaduration`, "expected duration"},
		{`runtime.noop:retry=abc`, "positive attempt count"},
		{`runtime.noop:dedup=yes`, "flag does not take a value"},
	} {
		issues := lintOne(t, tc.source)
		ktest.RequireLen(t, issues, 1)
		ktest.RequireEqual(t, issues[0].Kind, "compile")
		ktest.RequireStringContains(t, issues[0].Message, tc.message)
	}
}

func TestLintCompileRejectsUnknownModifierAndAtom(t *testing.T) {
	for _, src := range []string{
		`runtime.noop:timetout=5s`,
		`does.not.exist`,
	} {
		issues := lintOne(t, src)
		ktest.RequireLen(t, issues, 1)
		ktest.RequireEqual(t, issues[0].Kind, "compile")
	}
}

func TestLintCompileRejectsParseError(t *testing.T) {
	issues := lintOne(t, `runtime.noop -> )`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "compile")
}

func TestLintCompileReportsMacroSyntaxError(t *testing.T) {
	src := `@macro broken() { noop -> ) }
@broken()
`
	issues := lintOne(t, src)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "compile")
	ktest.RequireStringContains(t, issues[0].Message, "in macro @broken")
	ktest.RequireStringContains(t, issues[0].Message, "defined at")
}
