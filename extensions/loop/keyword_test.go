package loop

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// newTestParser wires a parser with only this extension's primary table,
// so anything the keyword produces is attributable to this package.
func newTestParser(tb testing.TB, source string) *core.Parser {
	tb.Helper()
	return core.NewParserWithFileOffset(
		tb.Context(),
		core.NewOperatorTable(),
		core.NewPrimaryExtensionTable(loopKeyword{}),
		source,
		"<test>",
		0,
	)
}

func TestKeyword_Identity(t *testing.T) {
	t.Parallel()
	kw := loopKeyword{}
	ktest.RequireEqual(t, kw.Name(), "loop")
	ktest.RequireEqual(t, kw.Keyword(), "loop")
}

func TestParse_ProducesLoopExpr(t *testing.T) {
	t.Parallel()
	expr, err := newTestParser(t, `loop(noop) until(.n >= 3)`).Parse()
	ktest.RequireNoError(t, err)

	loop, ok := expr.(*core.LoopExpr)
	if !ok {
		t.Fatalf("got %T, want *core.LoopExpr", expr)
	}
	ktest.RequireEqual(t, loop.Until, ".n >= 3")
	ktest.RequireCondition(t, loop.Body != nil, "Body is nil")
}

func TestParse_UntilConditionPreserved(t *testing.T) {
	t.Parallel()
	// Commas, nested parens, and quoted strings inside the until
	// expression must survive as written — the runtime compiles them
	// with expr-lang, not the flow parser.
	const cond = `contains(["a","b"], .name) && (.n >= 3)`
	expr, err := newTestParser(t, `loop(noop) until(`+cond+`)`).Parse()
	ktest.RequireNoError(t, err)

	loop, ok := expr.(*core.LoopExpr)
	if !ok {
		t.Fatalf("got %T, want *core.LoopExpr", expr)
	}
	ktest.RequireEqual(t, loop.Until, cond)
}

func TestParse_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		wantSub string
	}{
		{"missing open paren", `loop noop until(.n >= 3)`, "expected '('"},
		{"missing close paren on body", `loop(noop until(.n >= 3)`, "expected ')'"},
		{"missing until", `loop(noop)`, "expected 'until'"},
		{"missing until open paren", `loop(noop) until .n >= 3`, "expected '('"},
		{"unclosed until paren", `loop(noop) until(.n >= 3`, "unclosed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := newTestParser(t, c.source).Parse()
			ktest.RequireErrorContains(t, err, c.wantSub)
		})
	}
}
