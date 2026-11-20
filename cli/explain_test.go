package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/modifiers_core"
	"github.com/nexssp/flow/extensions/modifiers_meta"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/scope"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func explainConfig(t *testing.T) runner.Config {
	return explainConfigWithBundles(t)
}

func explainConfigWithBundles(t *testing.T, extra ...compiler.Bundle) runner.Config {
	t.Helper()
	bundles := []compiler.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		projection.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		scope.Bundle(nil),
	}
	bundles = append(bundles, extra...)
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)
	return cfg
}

func explainSource(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	ktest.RequireNoError(t, renderExplain(&buf, explainConfig(t), src, "test.nflow"))
	return buf.String()
}

func TestExplain_DirectValueWithoutModifiers(t *testing.T) {
	out := explainSource(t, `"ok"`)
	ktest.RequireStringContains(t, out, "literal")
	ktest.RequireStringContains(t, out, "(no modifiers)")

	valueOut := explainSource(t, `@arg.1`)
	ktest.RequireStringContains(t, valueOut, "value")
	ktest.RequireStringContains(t, valueOut, "(no modifiers)")
}

func TestExplainAST_ValueExprTextAndJSON(t *testing.T) {
	cfg := explainConfig(t)
	source := `@arg.1`

	var text bytes.Buffer
	if err := renderExplainASTWithContext(context.Background(), &text, cfg, source, "value.nflow", false); err != nil {
		t.Fatal(err)
	}
	ktest.RequireStringContains(t, text.String(), "Value")
	ktest.RequireStringContains(t, text.String(), ".arg.1")

	var encoded bytes.Buffer
	if err := renderExplainASTWithContext(context.Background(), &encoded, cfg, source, "value.nflow", true); err != nil {
		t.Fatal(err)
	}
	var report ASTReport
	if err := json.Unmarshal(encoded.Bytes(), &report); err != nil {
		t.Fatalf("decode AST JSON: %v\n%s", err, encoded.String())
	}
	if report.TopLevel == nil || report.TopLevel.Kind != "Value" || report.TopLevel.Name != ".arg.1" {
		t.Fatalf("AST JSON top-level node = %#v, want Value .arg.1", report.TopLevel)
	}
	if report.TopLevel.Pos == nil || report.TopLevel.Pos.File != "value.nflow" || report.TopLevel.Pos.Line != 1 || report.TopLevel.Pos.Col < 1 {
		t.Fatalf("AST JSON value position = %#v, want value.nflow:1 with a positive column", report.TopLevel.Pos)
	}
}

func TestExplain_AtomWithLocalModifier(t *testing.T) {
	out := explainSource(t, `noop:timeout=5s`)
	ktest.RequireStringContains(t, out, ":timeout=5s")
	ktest.RequireStringContains(t, out, "from atom")
}

func TestExplain_ExamplePayloadVisibleOnActionAndPipeline(t *testing.T) {
	out := explainSource(t, `
@pipeline lookup :example='{"account_id":"acct-demo"}'
  noop:example='{"value":"body-sample"}'
@end

noop:example='{"value":"top-level-sample"}'
`)
	for _, want := range []string{
		`:example={"account_id":"acct-demo"}`,
		`:example={"value":"body-sample"}`,
		`:example={"value":"top-level-sample"}`,
	} {
		ktest.RequireStringContains(t, out, want)
	}
}

func TestExplain_ScopeAttribution(t *testing.T) {
	out := explainSource(t, `@scope :timeout=5s {
  noop
}`)
	ktest.RequireStringContains(t, out, ":timeout=5s")
	ktest.RequireStringContains(t, out, "from scope")
}

func TestExplain_PipelineWrapperAndBody(t *testing.T) {
	out := explainSource(t, `
@pipeline work :tag="wrapper-only" :timeout=5s
  noop
@end

{} -> pipeline.work
`)
	ktest.RequireStringContains(t, out, "pipeline.work")
	ktest.RequireStringContains(t, out, ":tag=wrapper-only")
	ktest.RequireStringContains(t, out, "from pipeline work")
	ktest.RequireStringContains(t, out, ":timeout=5s")
}

func TestExplain_ProfileAttribution(t *testing.T) {
	out := explainSource(t, `
@profile fast :timeout=5s

@pipeline work :profile=fast
  noop
@end

{} -> pipeline.work
`)
	ktest.RequireStringContains(t, out, ":timeout=5s")
	ktest.RequireStringContains(t, out, "from profile fast")
}
