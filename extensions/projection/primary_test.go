package projection

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func newProjectionParser(tb testing.TB, source string) *core.Parser {
	tb.Helper()
	return core.NewParserWithFileOffset(
		tb.Context(),
		core.NewOperatorTable(),
		core.NewPrimaryExtensionTable(Extension{}),
		source,
		"<test>",
		0,
	)
}

func TestPrimary_Identity(t *testing.T) {
	p := Extension{}
	ktest.RequireEqual(t, p.Name(), "projection")
	ktest.RequireEqual(t, p.TokenType(), core.TokLBrace)
}

func TestParse_SimpleProjection(t *testing.T) {
	expr, err := newProjectionParser(t, `{ name: .name }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*core.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *core.ProjectionExpr", expr)
	ktest.RequireEqual(t, proj.Raw, "name: .name")
}

func TestParse_NestedBraces(t *testing.T) {
	expr, err := newProjectionParser(t, `{ a: { b: 1 } }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*core.ProjectionExpr)
	if !ok {
		t.Fatalf("expected *core.ProjectionExpr, got %T", expr)
	}
	ktest.RequireEqual(t, proj.Raw, "a: { b: 1 }")
}

func TestParse_BracesInsideQuotes(t *testing.T) {
	expr, err := newProjectionParser(t, `{ a: "}" }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*core.ProjectionExpr)
	if !ok {
		t.Fatalf("expected *core.ProjectionExpr, got %T", expr)
	}
	ktest.RequireEqual(t, proj.Raw, `a: "}"`)
}

func TestParse_Unclosed(t *testing.T) {
	_, err := newProjectionParser(t, `{ a: 1`).Parse()
	ktest.RequireErrorContains(t, err, "unclosed projection")
}
