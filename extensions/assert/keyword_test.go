package assert

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func newTestParser(tb testing.TB, source string) *core.Parser {
	tb.Helper()
	return core.NewParserWithFileOffset(
		tb.Context(),
		core.NewOperatorTable(),
		core.NewPrimaryExtensionTable(assertKeyword{}),
		source,
		"<test>",
		0,
	)
}

func TestKeyword_Identity(t *testing.T) {
	t.Parallel()
	kw := assertKeyword{}
	ktest.RequireEqual(t, kw.Name(), "assert")
	ktest.RequireEqual(t, kw.Keyword(), "assert")
}

func TestParse_SingleArgument(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `assert(.score >= 50)`).Parse()
	ktest.RequireNoError(t, err)

	a, ok := expr.(*core.AssertExpr)
	if !ok {
		t.Fatalf("got %T, want *core.AssertExpr", expr)
	}
	ktest.RequireEqual(t, a.Condition, ".score >= 50")
	ktest.RequireEqual(t, a.Message, "")
}

func TestParse_ConditionAndMessage(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `assert(.score >= 50, "score too low")`).Parse()
	ktest.RequireNoError(t, err)

	a, ok := expr.(*core.AssertExpr)
	if !ok {
		t.Fatalf("got %T, want *core.AssertExpr", expr)
	}
	ktest.RequireEqual(t, a.Condition, ".score >= 50")
	ktest.RequireEqual(t, a.Message, "score too low")
}

func TestParse_CommaInsideFunctionCallPreserved(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `assert(contains(["a","b"], .x), "missing")`).Parse()
	ktest.RequireNoError(t, err)

	a, ok := expr.(*core.AssertExpr)
	if !ok {
		t.Fatalf("got %T, want *core.AssertExpr", expr)
	}
	ktest.RequireEqual(t, a.Condition, `contains(["a","b"], .x)`)
	ktest.RequireEqual(t, a.Message, "missing")
}

func TestParse_MessageQuoteStyles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"double", `assert(x, "msg")`, "msg"},
		{"single", `assert(x, 'msg')`, "msg"},
		{"backtick", "assert(x, `msg`)", "msg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			expr, err := newTestParser(t, c.input).Parse()
			ktest.RequireNoError(t, err)
			a, ok := expr.(*core.AssertExpr)
			if !ok {
				t.Fatalf("got %T, want *core.AssertExpr", expr)
			}
			ktest.RequireEqual(t, a.Message, c.want)
		})
	}
}

func TestParse_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		wantSub string
	}{
		{"missing open paren", `assert .score >= 50`, "expected '('"},
		{"unclosed paren", `assert(.score >= 50`, "unclosed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := newTestParser(t, c.source).Parse()
			ktest.RequireErrorContains(t, err, c.wantSub)
		})
	}
}
