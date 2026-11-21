package compiler

import (
	"context"
	"strings"
	"testing"
)

func newTestParser(t testing.TB, src string) *Parser {
	t.Helper()
	return NewParser(context.Background(), NewOperatorTable(), src)
}

func TestParseAtBrace_ScalarValues(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap ( key: "outer", count: 3, enabled: true, empty: null )`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if len(atom.Args) != 4 {
		t.Fatalf("args count = %d, want 4", len(atom.Args))
	}
	if v := atom.Args["key"]; v.Kind != ValueString || v.Str != "outer" {
		t.Errorf("key = %+v, want string outer", v)
	}
	if v := atom.Args["count"]; v.Kind != ValueNumber || v.Num != 3 {
		t.Errorf("count = %+v, want number 3", v)
	}
	if v := atom.Args["enabled"]; v.Kind != ValueBool || !v.Bool {
		t.Errorf("enabled = %+v, want true", v)
	}
	if v := atom.Args["empty"]; v.Kind != ValueNull {
		t.Errorf("empty = %+v, want null", v)
	}
}

func TestParseAtBraceNumericSeparators(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap(count: 1_123_123, ratio: 1_234.5_6)`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("parsed expression has type %T, want *Atom", parsed)
	}
	if got := atom.Args["count"]; got.Kind != ValueNumber || got.Num != 1123123 {
		t.Fatalf("count = %+v, want 1123123", got)
	}
	if got := atom.Args["ratio"]; got.Kind != ValueNumber || got.Num != 1234.56 {
		t.Fatalf("ratio = %+v, want 1234.56", got)
	}
}

func TestParseAtBrace_NestedObjectAndSlice(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap ( meta: { retry: 3, tags: ["a", "b"] } )`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	meta := atom.Args["meta"]
	if meta.Kind != ValueMap || len(meta.Map) != 2 {
		t.Fatalf("meta = %+v, want map with 2 entries", meta)
	}
	var retry, tags *Value
	for _, e := range meta.Map {
		switch e.Key {
		case "retry":
			retry = e.Value
		case "tags":
			tags = e.Value
		}
	}
	if retry == nil || retry.Kind != ValueNumber || retry.Num != 3 {
		t.Errorf("retry = %+v, want number 3", retry)
	}
	if tags == nil || tags.Kind != ValueSlice || len(tags.Slice) != 2 {
		t.Errorf("tags = %+v, want slice of 2", tags)
	}
}

func TestParseAtBrace_References(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap ( from: .user.name, whole: . )`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	args := atom.Args
	if v := args["from"]; v.Kind != ValueRef || v.Ref != "user.name" {
		t.Errorf("from = %+v, want ref user.name", v)
	}
	if v := args["whole"]; !v.IsWholeStateRef() {
		t.Errorf("whole = %+v, want whole-state ref", v)
	}
}

func TestParseAtBrace_SameNameFieldShorthand(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap ( name, email, profile: { account_id } )`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("parsed expression has type %T, want *Atom", parsed)
	}
	for _, key := range []string{"name", "email"} {
		value := atom.Args[key]
		if value == nil || value.Kind != ValueRef || value.Ref != key {
			t.Errorf("%s = %+v, want same-name reference", key, value)
		}
	}
	profile := atom.Args["profile"]
	if profile == nil || profile.Kind != ValueMap || len(profile.Map) != 1 ||
		profile.Map[0].Key != "account_id" || profile.Map[0].Value.Kind != ValueRef || profile.Map[0].Value.Ref != "account_id" {
		t.Fatalf("profile = %+v, want nested same-name reference", profile)
	}
}

func TestParseAtBrace_CollectsArgumentMapSpreads(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap ( ... .defaults, url: .url )`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("parsed expression has type %T, want *Atom", parsed)
	}
	if len(atom.ArgSpreads) != 1 || atom.ArgSpreads[0].Kind != ValueRef || atom.ArgSpreads[0].Ref != "defaults" {
		t.Fatalf("spreads = %+v, want one .defaults spread", atom.ArgSpreads)
	}
	if value := atom.Args["url"]; value == nil || value.Kind != ValueRef || value.Ref != "url" {
		t.Fatalf("url = %+v, want .url reference", value)
	}
}

func TestParseAtBrace_CommentsAndQuotedCommentMarkers(t *testing.T) {
	t.Parallel()
	src := "wrap (\n" +
		"  full_tree: true, # closing parenthesis here must not end the map )\n" +
		"  text: \"# literal // literal )\", // ) also ignored\n" +
		"  depth: 3 # final value comment\n" +
		")"
	parsed, err := newTestParser(t, src).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("parsed expression has type %T, want *Atom", parsed)
	}
	if atom.Args["full_tree"].Kind != ValueBool || !atom.Args["full_tree"].Bool {
		t.Errorf("full_tree = %+v, want true", atom.Args["full_tree"])
	}
	if atom.Args["text"].Str != "# literal // literal )" {
		t.Errorf("text = %q, want quoted comment markers", atom.Args["text"].Str)
	}
	if atom.Args["depth"].Num != 3 {
		t.Errorf("depth = %+v, want 3", atom.Args["depth"])
	}
}

func TestParseAtBraceRecordsObjectPresence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{name: "pipeline call", src: `pipeline.lookup ( request: ., region: "eu" )`, want: true},
		{name: "imported pipeline call", src: `billing.pipeline.lookup ( request: . )`, want: true},
		{name: "empty pipeline call", src: `pipeline.lookup ( )`, want: true},
		{name: "generic action", src: `runtime.noop ( request: . )`, want: true},
		{name: "call without an object", src: `pipeline.lookup`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := newTestParser(t, tc.src).Parse()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			atom, ok := expr.(*Atom)
			if !ok {
				t.Fatalf("parsed expression has type %T, want *Atom", expr)
			}
			if atom.HasArgumentObject != tc.want {
				t.Errorf("HasArgumentObject = %t, want %t", atom.HasArgumentObject, tc.want)
			}
			if atom.NamedPipelineArgs {
				t.Error("parser should leave pipeline-specific lowering to the pipeline extension")
			}
		})
	}
}

func TestParseDirectScalarValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want any
		ref  string
	}{
		{name: "string", src: `"hello"`, want: "hello"},
		{name: "numeric-looking string", src: `"42"`, want: "42"},
		{name: "number", src: `42`, want: int64(42)},
		{name: "number with separators", src: `1_123_123`, want: int64(1123123)},
		{name: "negative number", src: `-7`, want: int64(-7)},
		{name: "decimal with separators", src: `-1_234.5_6`, want: float64(-1234.56)},
		{name: "boolean", src: `true`, want: true},
		{name: "false", src: `false`, want: false},
		{name: "null", src: `null`, want: nil},
		{name: "CLI flag reference", src: `@flag.name`, ref: "flag.name"},
		{name: "compile-time config reference", src: `@config.count`, ref: "config.count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr, err := newTestParser(t, tc.src).Parse()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tc.ref != "" {
				value, ok := expr.(*ValueExpr)
				if !ok || value.Value == nil || value.Value.Kind != ValueRef || value.Value.Ref != tc.ref {
					t.Fatalf("Parse(%q) = %#v, want ValueExpr reference %q", tc.src, expr, tc.ref)
				}
				return
			}
			literal, ok := expr.(*LiteralExpr)
			if !ok || literal.Val != tc.want {
				t.Fatalf("Parse(%q) = %#v, want literal %#v", tc.src, expr, tc.want)
			}
		})
	}
}

func TestParseObjectProjectionDirectly(t *testing.T) {
	t.Parallel()
	p := NewParserWithPrimaries(context.Background(), NewOperatorTable(), NewPrimaryExtensionTable(testProjectionPrimary{}), `{ count: 3, enabled: true }`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := expr.(*ProjectionExpr); !ok {
		t.Fatalf("parsed expression has type %T, want *ProjectionExpr", expr)
	}
}

func TestParseDirectValueRejectsMalformedNumber(t *testing.T) {
	t.Parallel()
	if _, err := newTestParser(t, `1.2.3`).Parse(); err == nil || !strings.Contains(err.Error(), "invalid numeric literal") {
		t.Fatal("Parse(1.2.3) succeeded, want invalid numeric literal error")
	}
}

func TestParseRejectsInvalidNumericSeparators(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`1__2`,
		`1_`,
		`wrap(count: 1_.2)`,
		`wrap(count: 1._2)`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if _, err := newTestParser(t, source).Parse(); err == nil || !strings.Contains(err.Error(), "invalid numeric literal") {
				t.Fatalf("Parse(%q) error = %v, want invalid numeric literal", source, err)
			}
		})
	}
}

func TestParseAtBrace_Empty(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap ( )`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if n := len(atom.Args); n != 0 {
		t.Errorf("args = %d, want 0", n)
	}
}

func TestParseAtBrace_AfterModifier(t *testing.T) {
	t.Parallel()
	p := newTestParser(t, `wrap:timeout=5s ( key: "outer" )`)
	expr, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := expr.(*Atom)
	if !ok {
		t.Fatalf("expected *Atom, got %T", expr)
	}
	if len(atom.Modifiers) != 1 || atom.Modifiers[0] != "timeout=5s" {
		t.Errorf("modifiers = %v, want [timeout=5s]", atom.Modifiers)
	}
	if v := atom.Args["key"]; v == nil || v.Str != "outer" {
		t.Errorf("key = %+v, want outer", v)
	}
}

func TestParseAtBrace_RejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		`wrap ( key value )`,
		`wrap ( key: )`,
		`wrap ( : "v" )`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			if _, err := newTestParser(t, src).Parse(); err == nil {
				t.Fatalf("expected error for %q", src)
			}
		})
	}
}

func TestParseAtBrace_QuotedBracesAndDuplicateKeys(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap ( text: "{ nested }" )`).Parse()
	if err != nil {
		t.Fatalf("quoted braces should not close the argument block: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("parsed expression has type %T, want *Atom", parsed)
	}
	if got := atom.Args["text"].Str; got != "{ nested }" {
		t.Fatalf("text = %q, want quoted braces", got)
	}
	for _, src := range []string{
		`wrap ( key: 1, key: 2 )`,
		`wrap ( outer: { key: 1, key: 2 } )`,
		`wrap ( key, key: 2 )`,
		`wrap(key=1, key=2)`,
	} {
		if _, err := newTestParser(t, src).Parse(); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("Parse(%q) error = %v, want duplicate-key/parameter error", src, err)
		}
	}
}

func TestParseRejectsMultipleNamedArgumentObjects(t *testing.T) {
	t.Parallel()
	_, err := newTestParser(t, `wrap ( key: 1 ) ( key: 2 )`).Parse()
	if err == nil || !strings.Contains(err.Error(), "already has a named argument object") {
		t.Fatalf("Parse() error = %v, want duplicate-object error", err)
	}
}

func TestLexerAtBrace_RawBacktickCloseAfterBackslash(t *testing.T) {
	t.Parallel()

	source := "wrap @{ value: raw(`raw\\`) }"
	lexer := NewLexer(source)
	lexer.Next() // wrap
	token := lexer.Next()
	if token.Type != TokAtBrace {
		t.Fatalf("token type = %v, want TokAtBrace", token.Type)
	}
	if !strings.Contains(token.Lit, "value: raw(") {
		t.Fatalf("legacy argument token content = %q, want the expression after the raw-string boundary", token.Lit)
	}
	if token.EndOffset != len(source) {
		t.Fatalf("EndOffset = %d, want %d", token.EndOffset, len(source))
	}
}

func TestParseRejectsUnterminatedStringOrArgumentBlock(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`"unterminated`, `wrap(key: "unterminated)`} {
		if _, err := newTestParser(t, src).Parse(); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", src)
		}
	}
}

func TestParseLimitsNestedExpressionDepth(t *testing.T) {
	t.Parallel()
	src := strings.Repeat("(", maxParseDepth+1) + "noop" + strings.Repeat(")", maxParseDepth+1)
	if _, err := newTestParser(t, src).Parse(); err == nil || !strings.Contains(err.Error(), "parse depth exceeded") {
		t.Fatalf("Parse(deep input) error = %v, want parse-depth diagnostic", err)
	}
}

func TestParseLimitsNestedArgumentDepth(t *testing.T) {
	t.Parallel()
	src := "wrap(value: " + strings.Repeat("{value: ", maxParseDepth+1) + "null" + strings.Repeat("}", maxParseDepth+1) + ")"
	if _, err := newTestParser(t, src).Parse(); err == nil || !strings.Contains(err.Error(), "parse depth exceeded") {
		t.Fatalf("Parse(deep argument) error = %v, want parse-depth diagnostic", err)
	}
}

func TestParseAtBrace_PathShorthandDerivesFinalSegment(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap ( .current.attempt.attempt_id, nested: { .user.profile.email } )`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("Parse() returned %T, want *Atom", parsed)
	}
	value := atom.Args["attempt_id"]
	if value == nil || value.Kind != ValueRef || value.Ref != "current.attempt.attempt_id" {
		t.Fatalf("attempt_id = %+v, want reference current.attempt.attempt_id", value)
	}
	nested := atom.Args["nested"]
	if nested == nil || nested.Kind != ValueMap || len(nested.Map) != 1 || nested.Map[0].Key != "email" || nested.Map[0].Value.Ref != "user.profile.email" {
		t.Fatalf("nested = %+v, want email reference user.profile.email", nested)
	}
}

func TestParseAtBrace_PathShorthandRejectsDuplicateDerivedKeys(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`wrap ( .user.id, id: .other )`,
		`wrap ( id: .other, .user.id )`,
		`wrap ( nested: { .user.id, id: .other } )`,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := newTestParser(t, source).Parse()
			if err == nil || !strings.Contains(err.Error(), `duplicate key "id"`) {
				t.Fatalf("parse error = %v, want duplicate derived key", err)
			}
		})
	}
}

func TestParseAtBrace_PathShorthandPreservesArgumentSpread(t *testing.T) {
	t.Parallel()
	parsed, err := newTestParser(t, `wrap ( ... .defaults, .current.attempt.attempt_id )`).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("Parse() returned %T, want *Atom", parsed)
	}
	if len(atom.ArgSpreads) != 1 || atom.ArgSpreads[0].Kind != ValueRef || atom.ArgSpreads[0].Ref != "defaults" {
		t.Fatalf("spreads = %+v, want the existing .defaults spread", atom.ArgSpreads)
	}
	value := atom.Args["attempt_id"]
	if value == nil || value.Kind != ValueRef || value.Ref != "current.attempt.attempt_id" {
		t.Fatalf("attempt_id = %+v, want reference current.attempt.attempt_id", value)
	}
}

func TestParseAtBrace_ImplicitExpressionsAndBareValues(t *testing.T) {
	t.Parallel()
	source := `wrap (
		expression: 1 + 2,
		condition: .count > 1 ? true : false,
		nested: { computed: input.count + 1 },
		items: [person.count + 1, input.count * 2],
		...{ value: len([")", "(", "}"]) > 0 },
		bare_input: input.name,
		bare_binding: person,
		bare_path: person.name,
		quoted: "person.name",
		current: .,
		explicit: (input.count + 3),
		greeting: "Hello, " + .name,
		length: len(.tags),
		qualified: whole_value.echo(input.name)
	)`
	parsed, err := newTestParser(t, source).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("Parse() returned %T, want *Atom", parsed)
	}

	assertExpression := func(name string, value *Value, want string) {
		t.Helper()
		if value == nil || value.Kind != ValueExpression || value.Expression != want {
			t.Errorf("%s = %+v, want expression %q", name, value, want)
		}
	}
	assertExpression("expression", atom.Args["expression"], "1 + 2")
	assertExpression("condition", atom.Args["condition"], ".count > 1 ? true : false")
	assertExpression("nested expression", atom.Args["nested"].Map[0].Value, "input.count + 1")
	assertExpression("slice expression", atom.Args["items"].Slice[0], "person.count + 1")
	assertExpression("second slice expression", atom.Args["items"].Slice[1], "input.count * 2")
	if len(atom.ArgSpreads) != 1 || atom.ArgSpreads[0].Kind != ValueMap {
		t.Fatalf("argument spreads = %+v, want one map spread", atom.ArgSpreads)
	}
	assertExpression("spread expression", atom.ArgSpreads[0].Map[0].Value, `len([")", "(", "}"]) > 0`)
	assertExpression("explicit expression", atom.Args["explicit"], "input.count + 3")
	assertExpression("string concatenation", atom.Args["greeting"], `"Hello, " + .name`)
	assertExpression("unqualified function call", atom.Args["length"], "len(.tags)")

	for name, want := range map[string]string{
		"bare_input":   "input.name",
		"bare_binding": "person",
		"bare_path":    "person.name",
	} {
		value := atom.Args[name]
		if value == nil || value.Kind != ValueBare || value.Str != want {
			t.Errorf("%s = %+v, want unchanged bare value %q", name, value, want)
		}
	}
	if value := atom.Args["quoted"]; value == nil || value.Kind != ValueString || value.Str != "person.name" {
		t.Errorf("quoted = %+v, want unchanged string value", value)
	}
	if value := atom.Args["current"]; value == nil || value.Kind != ValueRef || value.Ref != "" {
		t.Errorf("current = %+v, want unchanged whole-state reference", value)
	}
	if value := atom.Args["qualified"]; value == nil || value.Kind != ValueExpression || value.Expression != "whole_value.echo(input.name)" {
		t.Errorf("qualified = %+v, want existing qualified function expression", value)
	}
}

func TestParseAtBrace_ParenthesizedExpressionsIgnoreCommentDelimiters(t *testing.T) {
	t.Parallel()
	source := `wrap (
			block: (1 /* ) ] } " */ + 2),
			line: (1 + // ) ] }
			  2),
			hash: (1 +
			  # ) ] } ignored
			  2)
		)`
	parsed, err := newTestParser(t, source).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	atom, ok := parsed.(*Atom)
	if !ok {
		t.Fatalf("Parse() returned %T, want *Atom", parsed)
	}
	for _, name := range []string{"block", "line", "hash"} {
		value := atom.Args[name]
		if value == nil || value.Kind != ValueExpression {
			t.Fatalf("%s = %+v, want expression", name, value)
		}
	}
	if got := atom.Args["block"].Expression; got != `1 /* ) ] } " */ + 2` {
		t.Errorf("block expression = %q, want comment preserved and delimiters ignored", got)
	}
	if got := atom.Args["line"].Expression; !strings.Contains(got, "// ) ] }") || !strings.Contains(got, "\n") {
		t.Errorf("line expression = %q, want line comment preserved and delimiters ignored", got)
	}
	if got := atom.Args["hash"].Expression; strings.Contains(got, "#") || !strings.Contains(got, "1 +") || !strings.Contains(got, "2") {
		t.Errorf("hash expression = %q, want comment removed and expression retained", got)
	}
}

func TestParseAtBrace_ParenthesizedExpressionDelimiterErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		want   string
	}{
		{source: `wrap ( value: () )`, want: "empty parenthesized expression"},
		{source: `wrap ( value: (1 + 2 )`, want: "expected ')'"},
		{source: `wrap ( value: ([1) )`, want: "unclosed delimiter in parenthesized expression"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			_, err := newTestParser(t, tc.source).Parse()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse(%q) error = %v, want diagnostic containing %q", tc.source, err, tc.want)
			}
		})
	}
}
