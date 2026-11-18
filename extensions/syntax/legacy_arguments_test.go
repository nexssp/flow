package syntax_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestLegacyArgumentCompileErrorFixture(t *testing.T) {
	t.Parallel()

	const name = "invalid_legacy_argument.nflow"
	fixture, err := fs.ReadFile(syntaxext.Bundle(nil).Fixtures, "nflows/"+name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want := expectedLegacyArgumentCompileError(t, string(fixture))
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	_, err = runner.Compile(t.Context(), cfg, string(fixture), name)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("compile error = %v, want diagnostic containing %q", err, want)
	}
	wantPosition := compiler.Position{File: name, Line: 3, Col: 6}
	if got, ok := compiler.PositionOf(err); !ok || got != wantPosition {
		t.Fatalf("compile error position = %v, %t; want %v", got, ok, wantPosition)
	}
}

func expectedLegacyArgumentCompileError(t *testing.T, source string) string {
	t.Helper()
	const prefix = "# @expect-compile-error "
	for line := range strings.SplitSeq(source, "\n") {
		if diagnostic, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSpace(diagnostic)
		}
	}
	t.Fatal("fixture is missing its @expect-compile-error diagnostic")
	return ""
}
