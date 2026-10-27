package formatter

import (
	"os"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	requireext "github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

func testFormatter() *Formatter { return New(native.Bundles()) }

func TestFormatIsIdempotentAndNormalizesModifiers(t *testing.T) {
	input := "action:timeout=1s:retry=2\n"
	want := "action :timeout=1s :retry=2\n"
	got, err := testFormatter().Format(t.Context(), input, "fixture.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
	second, err := testFormatter().Format(t.Context(), got, "fixture.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent: %q != %q", second, got)
	}
}

func TestFormatSplitsPipelineArrowsDeterministically(t *testing.T) {
	input := "source->action:timeout=2s->finish\n"
	want := "source\n  -> action :timeout=2s\n  -> finish\n"
	got, err := testFormatter().Format(t.Context(), input, "pipeline.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
	second, err := testFormatter().Format(t.Context(), got, "pipeline.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent: %q != %q", second, got)
	}
}

func TestFormatDeclarationHeadersKeepsModifierOrderAndComments(t *testing.T) {
	input := "@profile reliable:timeout=2s:retry=4\n@pipeline fetch:profile=reliable\n  # header note\n  :tag=api:retry=2\n  http.request @{ url: \"https://api.example/x:y\" }\n@end\n"
	want := "@profile reliable\n  :timeout=2s\n  :retry=4\n@pipeline fetch\n  :profile=reliable\n  # header note\n  :tag=api\n  :retry=2\n  http.request @{ url: \"https://api.example/x:y\" }\n@end\n"
	got, err := testFormatter().Format(t.Context(), input, "headers.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Format() =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatPreservesStringsCommentsAndExpressionText(t *testing.T) {
	input := "// comment: keep\nservice.call:desc=\"Maks: ingest, Julia: verify\" @{ text: \"x:y -> z\" } # tail\n"
	want := "// comment: keep\nservice.call :desc=\"Maks: ingest, Julia: verify\" @{ text: \"x:y -> z\" } # tail\n"
	got, err := testFormatter().Format(t.Context(), input, "strings.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestHashCommentDetectionMatchesCompilerLexer(t *testing.T) {
	for _, tt := range []struct {
		text string
		want bool
	}{
		{text: "foo# bar -> baz", want: true},
		{text: "  #comment", want: true},
		{text: "foo #bar", want: false},
		{text: "foo#", want: false},
	} {
		if got := hasSourceComment(tt.text); got != tt.want {
			t.Errorf("hasSourceComment(%q) = %t, want %t", tt.text, got, tt.want)
		}
	}

	input := "foo# bar -> baz\n"
	got, err := testFormatter().Format(t.Context(), input, "hash-comment.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("Format() changed source following a hash comment: %q", got)
	}
}

func TestFormatPreservesTernarySeparator(t *testing.T) {
	input := "runtime.const @{ value: condition ? yes : no }\n"
	got, err := testFormatter().Format(t.Context(), input, "ternary.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "yes : no") {
		t.Fatalf("ternary separator changed: %q", got)
	}
}

func TestFormatObjectFieldColonUsesSpaceAfter(t *testing.T) {
	got, err := testFormatter().Format(t.Context(), "{field:.input}\n", "object.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != "{field: .input}\n" {
		t.Fatalf("object field was formatted like a modifier: %q", got)
	}
}

func TestFormatReportsExpressionParseLocation(t *testing.T) {
	_, err := testFormatter().Format(t.Context(), "first\n\n(\n", "broken.nflow")
	if err == nil || !strings.Contains(err.Error(), "broken.nflow:3") {
		t.Fatalf("expected file and line parse error, got %v", err)
	}
}

func TestFormatReportsDeprecatedPipelineSchemaAtModifierLocation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		line   int
	}{
		{
			name:   "inline",
			source: "@pipeline create_task:profile=worker:schema=TaskRequest\n  runtime.noop\n@end\n",
			line:   1,
		},
		{
			name:   "continuation",
			source: "@pipeline create_task\n  :profile=worker\n  :schema=TaskRequest\n@end\n",
			line:   3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testFormatter().Format(t.Context(), tt.source, "legacy.nflow")
			if err == nil {
				t.Fatal("Format() accepted deprecated :schema modifier")
			}
			want := compiler.Position{File: "legacy.nflow", Line: tt.line}
			line := strings.Split(tt.source, "\n")[tt.line-1]
			want.Col = strings.Index(line, ":schema") + 1
			got, ok := compiler.PositionOf(err)
			if !ok || got != want {
				t.Fatalf("PositionOf(error) = %+v, %t; want %+v (error: %v)", got, ok, want, err)
			}
			if !strings.Contains(err.Error(), ":schema is no longer supported; use :request-schema=Name") {
				t.Fatalf("error %q lacks actionable migration hint", err)
			}
		})
	}
}

func TestFormatPastedTaskAPIExampleReportsLegacySchemaAfterValidRequireBlocks(t *testing.T) {
	source, err := os.ReadFile("testdata/taskapi_http_nats.nflow")
	if err != nil {
		t.Fatal(err)
	}
	filename := "taskapi_http_nats.nflow"
	_, _, err = compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(requireext.Directive), string(source), filename)
	if err != nil {
		t.Fatalf("the real @require directive rejected the example options: %v", err)
	}

	_, err = testFormatter().Format(t.Context(), string(source), filename)
	if err == nil {
		t.Fatal("Format() accepted the pasted example's deprecated pipeline :schema modifiers")
	}
	if !strings.Contains(err.Error(), ":schema is no longer supported; use :request-schema=Name") {
		t.Fatalf("Format() reported the wrong error before reaching the legacy modifier: %v", err)
	}
	sourceText := string(source)
	firstSchema := strings.Index(sourceText, ":schema=")
	if firstSchema < 0 {
		t.Fatal("fixture no longer contains its documented legacy :schema modifier")
	}
	lineStart := strings.LastIndex(sourceText[:firstSchema], "\n") + 1
	want := compiler.Position{
		File: filename,
		Line: strings.Count(sourceText[:firstSchema], "\n") + 1,
		Col:  firstSchema - lineStart + 1,
	}
	got, ok := compiler.PositionOf(err)
	if !ok || got != want {
		t.Fatalf("PositionOf(error) = %+v, %t; want %+v (error: %v)", got, ok, want, err)
	}
}

func TestFormatUsesRequireDirectiveParserForInlineAndMultilineOptions(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{{
		name:   "inline",
		source: "@require example.com/http as http { addr: \"127.0.0.1:8080\" }\nruntime.const @{ value: true }\n",
	}, {
		name:   "multiline",
		source: "@require example.com/http as http\n{\n  addr: \"127.0.0.1:8080\",\n  timeout: \"5s\"\n}\nruntime.const @{ value: true }\n",
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(requireext.Directive), tt.source, "require.nflow"); err != nil {
				t.Fatalf("the real @require directive rejected valid options: %v", err)
			}
			got, err := testFormatter().Format(t.Context(), tt.source, "require.nflow")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, `addr: "127.0.0.1:8080"`) {
				t.Fatalf("formatter changed or lost the option object: %q", got)
			}
			second, err := testFormatter().Format(t.Context(), got, "require.nflow")
			if err != nil {
				t.Fatal(err)
			}
			if second != got {
				t.Fatalf("format is not idempotent: %q != %q", second, got)
			}
		})
	}

	invalid := "@require example.com/http as http { addr \"127.0.0.1:8080\" }\n"
	if _, err := testFormatter().Format(t.Context(), invalid, "bad-require.nflow"); err == nil || !strings.Contains(err.Error(), "@require options") {
		t.Fatalf("formatter did not report malformed @require option syntax: %v", err)
	}
}

func TestFormatPastedTaskAPIExampleAfterSchemaMigration(t *testing.T) {
	source, err := os.ReadFile("testdata/taskapi_http_nats.nflow")
	if err != nil {
		t.Fatal(err)
	}
	migrated := strings.ReplaceAll(string(source), ":schema=", ":request-schema=")
	got, err := testFormatter().Format(t.Context(), migrated, "taskapi_http_nats.nflow")
	if err != nil {
		t.Fatalf("Format() rejected the example after the documented schema migration: %v", err)
	}
	second, err := testFormatter().Format(t.Context(), got, "taskapi_http_nats.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent:\n%s\n!=\n%s", second, got)
	}
}

func TestFormatDoesNotTreatSchemaTextInHeaderValuesOrCommentsAsModifier(t *testing.T) {
	input := "@pipeline create_task:note=\"text :schema=legacy\" # :schema=comment\n  runtime.noop\n@end\n"
	if _, err := testFormatter().Format(t.Context(), input, "header.nflow"); err != nil {
		t.Fatalf("Format() rejected schema-like text in value/comment: %v", err)
	}
}

func TestFormatRejectsMissingPipelineEnd(t *testing.T) {
	_, err := testFormatter().Format(t.Context(), "@pipeline work\n  first\n", "pipeline.nflow")
	if err == nil || !strings.Contains(err.Error(), "pipeline.nflow:2") {
		t.Fatalf("expected source-positioned missing @end error, got %v", err)
	}
}

func TestFormatPreservesOpaqueDirectiveBody(t *testing.T) {
	input := "@macro rewrite() {\n  runtime.call->macro:opaque\n  # keep   \n}\n"
	got, err := testFormatter().Format(t.Context(), input, "macro.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("opaque directive body changed:\n got %q\nwant %q", got, input)
	}
}

func TestFormatPreservesOnErrorDirectiveBlock(t *testing.T) {
	input := "@on_error {\n" +
		"  when error.kind == \"Unavailable\" -> error.info\n" +
		"  when error.kind == \"BadRequest\" -> error.info\n" +
		"  else -> error.info\n" +
		"}\n\n" +
		"runtime.noop\n"
	got, err := testFormatter().Format(t.Context(), input, "on-error.nflow")
	if err != nil {
		t.Fatalf("Format() rejected a valid @on_error block: %v", err)
	}
	if !strings.Contains(got, "when error.kind") {
		t.Fatalf("Format() dropped extension-owned clauses: %q", got)
	}
	second, err := testFormatter().Format(t.Context(), got, "on-error.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent:\n%s\n!=\n%s", second, got)
	}
}

func TestFormatValidatesScopeExpressionInsidePipeline(t *testing.T) {
	input := "@pipeline scoped_example\n" +
		"  @scope :retry=3 :timeout=1s {\n" +
		"    const @{ value: \"scoped\" }\n" +
		"  }\n" +
		"  -> { scoped: .value }\n" +
		"@end\n"
	got, err := testFormatter().Format(t.Context(), input, "scope-expression.nflow")
	if err != nil {
		t.Fatalf("Format() rejected a valid scoped pipeline expression: %v", err)
	}
	second, err := testFormatter().Format(t.Context(), got, "scope-expression.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent:\n%s\n!=\n%s", second, got)
	}
}

func TestFormatTemplateRenderArgumentBlockWithBlankLines(t *testing.T) {
	template := `Hi {{.customer}},\n\nYour order {{.order_id}} is confirmed. Estimated delivery: {{.delivery_date}}.\n\nItems:\n{{range .items}}- {{.}}\n{{end}}\nThanks,\n{{.store}}`
	input := "@description \"Draft an order confirmation\"\n" +
		"@require template\n\n" +
		"template.render @{\n" +
		"  template: \"" + template + "\",\n" +
		"  variables: {\n" +
		"    customer: \"Ada Lovelace\",\n\n\n" +
		"    order_id: \"SO-1042\",\n" +
		"    delivery_date: \"Tuesday, Oct 28\",\n" +
		"    items: [\"Notebook\", \"Pen\"],\n" +
		"    store: \"Northstar\"\n" +
		"  }\n}\n"
	want := "@description \"Draft an order confirmation\"\n" +
		"@require template\n\n" +
		"template.render @{\n" +
		"  template: \"" + template + "\",\n" +
		"  variables: {\n" +
		"    customer: \"Ada Lovelace\",\n\n" +
		"    order_id: \"SO-1042\",\n" +
		"    delivery_date: \"Tuesday, Oct 28\",\n" +
		"    items: [\"Notebook\", \"Pen\"],\n" +
		"    store: \"Northstar\"\n" +
		"  }\n}\n"

	got, err := testFormatter().Format(t.Context(), input, "test-template.nflow")
	if err != nil {
		t.Fatalf("Format() rejected a valid multiline argument block: %v", err)
	}
	if got != want {
		t.Fatalf("Format() =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(got, `template: "`+template+`"`) {
		t.Fatal("formatter changed the quoted template text")
	}
	second, err := testFormatter().Format(t.Context(), got, "test-template.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if second != got {
		t.Fatalf("format is not idempotent:\n%s\n!=\n%s", second, got)
	}
}

func TestFormatReindentsNestedArgumentBraces(t *testing.T) {
	input := "runtime.const @{\nvalue: {\nanswer: 42\n}\n}\n"
	want := "runtime.const @{\n  value: {\n    answer: 42\n  }\n}\n"
	got, err := testFormatter().Format(t.Context(), input, "argument-block.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatPreservesMultilineProjectionText(t *testing.T) {
	input := "runtime.const @{ value: `first\n  second->third:keep\nlast` }\n"
	got, err := testFormatter().Format(t.Context(), input, "multiline.nflow")
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("multiline raw expression changed:\n got %q\nwant %q", got, input)
	}
}
