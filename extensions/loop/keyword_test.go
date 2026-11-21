package loop

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

// newTestParser wires a parser with only this extension's primary table,
// so anything the keyword produces is attributable to this package.
func newTestParser(tb testing.TB, source string) *compiler.Parser {
	tb.Helper()
	return compiler.NewParserWithFileOffset(
		tb.Context(),
		compiler.NewOperatorTable(),
		compiler.NewPrimaryExtensionTable(loopKeyword{}),
		source,
		"<test>",
		0,
	)
}

func newEachTestParser(tb testing.TB, source string) *compiler.Parser {
	tb.Helper()
	return compiler.NewParserWithFileOffset(
		tb.Context(),
		compiler.NewOperatorTable(),
		compiler.NewPrimaryExtensionTable(eachKeyword{}),
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

	loop, ok := expr.(*compiler.LoopExpr)
	if !ok {
		t.Fatalf("got %T, want *compiler.LoopExpr", expr)
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

	loop, ok := expr.(*compiler.LoopExpr)
	if !ok {
		t.Fatalf("got %T, want *compiler.LoopExpr", expr)
	}
	ktest.RequireEqual(t, loop.Until, cond)
}

func TestEachKeyword_IdentityAndBlock(t *testing.T) {
	t.Parallel()
	keyword := eachKeyword{}
	ktest.RequireEqual(t, keyword.Name(), eachKeywordName)
	ktest.RequireEqual(t, keyword.Keyword(), eachKeywordName)

	expr, err := newEachTestParser(t, `each { noop }`).Parse()
	ktest.RequireNoError(t, err)
	each, ok := expr.(*EachExpr)
	if !ok {
		t.Fatalf("got %T, want *EachExpr", expr)
	}
	ktest.RequireCondition(t, each.Body != nil, "Body is nil")
	ktest.RequireEqual(t, each.Concurrency, 0)
}

func TestEachKeyword_ConcurrencyBeforeOrAfterBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		want   int
	}{
		{source: `each :concurrency=4 { noop }`, want: 4},
		{source: `each :concurrency=1_000 { noop }`, want: 1000},
		{source: `each { noop } :concurrency=4`, want: 4},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			expr, err := newEachTestParser(t, tc.source).Parse()
			ktest.RequireNoError(t, err)
			each, ok := expr.(*EachExpr)
			if !ok {
				t.Fatalf("got %T, want *EachExpr", expr)
			}
			ktest.RequireEqual(t, each.Concurrency, tc.want)
		})
	}
}

func TestEachKeyword_QualifiedActionNameIsNotTheKeyword(t *testing.T) {
	t.Parallel()
	expr, err := newEachTestParser(t, `samples.each`).Parse()
	ktest.RequireNoError(t, err)
	atom, ok := expr.(*compiler.Atom)
	if !ok {
		t.Fatalf("got %T, want *compiler.Atom", expr)
	}
	ktest.RequireEqual(t, atom.Name, "samples.each")
}

func TestEachKeyword_ParseErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		wantSub string
	}{
		{"missing block", `each noop`, "expected '{'"},
		{"empty block", `each {}`, "block must contain"},
		{"missing concurrency value", `each :concurrency { noop }`, "expected '='"},
		{"non-integer concurrency", `each :concurrency=fast { noop }`, "must be an integer"},
		{"zero concurrency", `each :concurrency=0 { noop }`, "must be positive"},
		{"duplicate concurrency", `each :concurrency=2 { noop } :concurrency=3`, "duplicate :concurrency"},
		{"duplicate concurrency before block", `each :concurrency=2 :concurrency=3 { noop }`, "duplicate :concurrency"},
		{"duplicate concurrency after block", `each { noop } :concurrency=2 :concurrency=3`, "duplicate :concurrency"},
		{"unknown option before block", `each :timeout=1s { noop }`, "expected :concurrency=NUMBER"},
		{"other modifier after pre-block concurrency", `each :concurrency=2 :timeout=1s { noop }`, "only :concurrency may appear before the block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := newEachTestParser(t, tc.source).Parse()
			ktest.RequireErrorContains(t, err, tc.wantSub)
		})
	}
}

func TestLoopKeyword_ParseErrors(t *testing.T) {
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
		{"invalid character", `loop(noop) until(.n >= 3 §)`, "unexpected character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := newTestParser(t, tc.source).Parse()
			ktest.RequireErrorContains(t, err, tc.wantSub)
		})
	}
}
