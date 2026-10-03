package cli

import (
	"bytes"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
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
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		projection.Bundle(nil),
		modifiers_core.Bundle(nil),
		modifiers_meta.Bundle(nil),
		pipeline.Bundle(nil),
		scope.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func explainSource(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	ktest.RequireNoError(t, renderExplain(&buf, explainConfig(t), src, "test.nflow"))
	return buf.String()
}

func TestExplain_AtomWithoutModifiers(t *testing.T) {
	out := explainSource(t, `const @{ value: "ok" }`)
	ktest.RequireStringContains(t, out, "runtime.const")
	ktest.RequireStringContains(t, out, "(no modifiers)")
}

func TestExplain_AtomWithLocalModifier(t *testing.T) {
	out := explainSource(t, `noop:timeout=5s`)
	ktest.RequireStringContains(t, out, ":timeout=5s")
	ktest.RequireStringContains(t, out, "from atom")
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
