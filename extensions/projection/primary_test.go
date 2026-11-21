package projection

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func newProjectionParser(tb testing.TB, source string) *compiler.Parser {
	tb.Helper()
	return compiler.NewParserWithFileOffset(
		tb.Context(),
		compiler.NewOperatorTable(),
		compiler.NewPrimaryExtensionTable(Extension{}),
		source,
		"<test>",
		0,
	)
}

func TestPrimary_Identity(t *testing.T) {
	p := Extension{}
	ktest.RequireEqual(t, p.Name(), "projection")
	ktest.RequireEqual(t, p.TokenType(), compiler.TokLBrace)
}

func TestParse_SimpleProjection(t *testing.T) {
	expr, err := newProjectionParser(t, `{ name: .name }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireEqual(t, strings.TrimSpace(proj.Raw), "name: .name")
}

func TestParse_NestedBraces(t *testing.T) {
	expr, err := newProjectionParser(t, `{ a: { b: 1 } }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	if !ok {
		t.Fatalf("expected *compiler.ProjectionExpr, got %T", expr)
	}
	ktest.RequireEqual(t, strings.TrimSpace(proj.Raw), "a: { b: 1 }")
}

func TestParse_BracesInsideQuotes(t *testing.T) {
	expr, err := newProjectionParser(t, `{ a: "}" }`).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	if !ok {
		t.Fatalf("expected *compiler.ProjectionExpr, got %T", expr)
	}
	ktest.RequireEqual(t, strings.TrimSpace(proj.Raw), `a: "}"`)
}

func TestParse_BraceInsideRawStringAfterBackslash(t *testing.T) {
	src := "{ value: `raw\\` + `}` }"
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireEqual(t, strings.TrimSpace(proj.Raw), "value: `raw\\` + `}`")
}

func TestParse_Unclosed(t *testing.T) {
	_, err := newProjectionParser(t, `{ a: 1`).Parse()
	ktest.RequireErrorContains(t, err, "unclosed projection")
}

func TestParse_StripsHashComments(t *testing.T) {
	src := `{
  # leading comment
  name: .name,  # inline comment
  age: 42
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)

	ktest.RequireStringNotContains(t, proj.Raw, "#")
	ktest.RequireStringNotContains(t, proj.Raw, "comment")
	ktest.RequireStringContains(t, proj.Raw, "name: .name")
	ktest.RequireStringContains(t, proj.Raw, "age: 42")
}

func TestParse_HashInsideStringPreserved(t *testing.T) {
	src := `{ tag: "#not-a-comment" }`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T", expr)
	ktest.RequireStringContains(t, proj.Raw, `"#not-a-comment"`)
}

func TestParse_ClosingBraceInsideCommentIgnored(t *testing.T) {
	// A `}` inside a comment line must not close the projection early.
	src := `{ a: 1  # note: use } carefully
  b: 2 }`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T", expr)
	ktest.RequireStringContains(t, proj.Raw, "b: 2")
}

func TestParse_HashIteratorInsideParensIsKept(t *testing.T) {
	src := `{ top: items | filter(#.severity == "high") | sortBy(#.line, "desc") | take(2) }`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringContains(t, proj.Raw, `filter(#.severity == "high")`)
	ktest.RequireStringContains(t, proj.Raw, `sortBy(#.line, "desc")`)
	ktest.RequireStringContains(t, proj.Raw, `take(2)`)
}

func TestParse_MultilineHashMemberIteratorCompilesWithTypedProjection(t *testing.T) {
	src := `{
  mapped: .items | map(
    #.found
  )
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringContains(t, proj.Raw, `#.found`)

	type item struct {
		Found bool `json:"found"`
	}
	type input struct {
		Items []item `json:"items"`
	}
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[input]()},
	}
	if _, err := analyzeProjectionTypes(proj.Raw, compiler.Position{}, ctx, nil, nil, ""); err != nil {
		t.Fatalf("analyze multiline iterator projection: %v", err)
	}
}

func TestParse_MultilineHashIndexIteratorIsNotComment(t *testing.T) {
	src := `{
  selected: .items | map(
    #[0]
  )
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringContains(t, proj.Raw, `#[0]`)
}

func TestParse_HashWhitespaceBeforeIteratorPathRemainsComment(t *testing.T) {
	src := `{
  value: (1 +
    # . this remains a comment
    # [this also remains a comment]
    2)
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringNotContains(t, proj.Raw, `# . this remains a comment`)
	ktest.RequireStringNotContains(t, proj.Raw, `# [this also remains a comment]`)
}

func TestParse_MultilineOptionalAndBareHashIteratorsAreKept(t *testing.T) {
	src := `{
  optional: .items | map(
    #?.field
  ),
	filtered: .labels | filter(
	  # != "beta"
	  ),
	coalesced: .nullable | map(
	  # ?? "fallback"
	  ),
	conditional: .flags | map(
	  # ? "yes" : "no"
	  )
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringContains(t, proj.Raw, `#?.field`)
	ktest.RequireStringContains(t, proj.Raw, `# != "beta"`)

	input := map[string]any{
		"items":    []map[string]any{{"field": "alpha"}, {"field": "beta"}},
		"labels":   []string{"alpha", "beta", "gamma"},
		"nullable": []any{nil, "present"},
		"flags":    []bool{true, false},
	}
	got, err := NewExprEvaluator()(t.Context(), Request{Raw: proj.Raw, Input: input})
	if err != nil {
		t.Fatalf("evaluate multiline hash iterator projection: %v", err)
	}
	want := map[string]any{
		"optional":    []any{"alpha", "beta"},
		"filtered":    []any{"alpha", "gamma"},
		"coalesced":   []any{"fallback", "present"},
		"conditional": []any{"yes", "no"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection output = %#v, want %#v", got, want)
	}
}

func TestParse_StandaloneHashInsideParenthesesRemainsComment(t *testing.T) {
	src := `{
  value: (1 +
    #
    2)
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringNotContains(t, proj.Raw, `#`)
	ktest.RequireStringContains(t, proj.Raw, `2)`)
}

func TestParse_HashIteratorAndCommentCoexist(t *testing.T) {
	// Reproduces spec/08: a depth-0 comment on one line, an iterator
	// inside filter() on another. The comment is stripped, the iterator
	// survives, the projection closes correctly.
	src := `{
  ...,
  # comment on its own line
  clean_tags: .user.tags | filter(# != "beta") | map(upper(#))
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T", expr)

	ktest.RequireStringNotContains(t, proj.Raw, "comment on its own line")
	ktest.RequireStringContains(t, proj.Raw, `filter(# != "beta")`)
	ktest.RequireStringContains(t, proj.Raw, `map(upper(#))`)
}

func TestParse_CommentsInsideParenthesizedExpressionsDoNotCloseProjection(t *testing.T) {
	src := `{
  block: (1 /* ) ] } */ + 2),
  slash: (1 + // ) ] }
    2),
  hash: (1 +
    # ) ] } ignored
    2)
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)
	ktest.RequireStringContains(t, proj.Raw, "block: (1")
	ktest.RequireStringContains(t, proj.Raw, "slash: (1 +")
	ktest.RequireStringContains(t, proj.Raw, "hash: (1 +")
	ktest.RequireStringNotContains(t, proj.Raw, "/*")
	ktest.RequireStringNotContains(t, proj.Raw, "//")
	ktest.RequireStringNotContains(t, proj.Raw, "# )")
}

func TestParse_CRLFLineEndingPreserved(t *testing.T) {
	src := "{ a: 1, # comment\r\n b: 2 }"
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T", expr)
	ktest.RequireStringContains(t, proj.Raw, "\r\n")
	ktest.RequireStringNotContains(t, proj.Raw, "# comment")
}

// TestParse_LineAlignmentPreserved pins the invariant that raw is the
// verbatim source between the opening and closing braces. Removing
// leading whitespace shifts every line number expr-lang reports: a
// projection whose body starts at file line L+3 would report its
// compile errors as belonging to line 1 instead of line L+3.
//
// If this test fails, someone reintroduced strings.TrimSpace on raw
// inside Parse. Fix Parse, not this test.
func TestParse_LineAlignmentPreserved(t *testing.T) {
	src := `{
  # line 2 (comment, will be stripped)
  # line 3 (comment, will be stripped)
  name: .name,
  age: 42
}`
	expr, err := newProjectionParser(t, src).Parse()
	ktest.RequireNoError(t, err)

	proj, ok := expr.(*compiler.ProjectionExpr)
	ktest.RequireCondition(t, ok, "got %T, want *compiler.ProjectionExpr", expr)

	// The raw must contain exactly the same number of newlines as the
	// source it was cut from. Four newlines: after `{`, after each
	// comment line, and after `age: 42`.
	ktest.RequireEqual(t, strings.Count(proj.Raw, "\n"), 5)

	// Line 1 of raw is empty (right after `{`). Line 4 of raw is the
	// first non-comment, non-blank line.
	lines := strings.Split(proj.Raw, "\n")
	ktest.RequireStringContains(t, lines[3], "name: .name")
	ktest.RequireStringContains(t, lines[4], "age: 42")
}
