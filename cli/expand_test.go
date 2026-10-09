package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

// ── collectExpansions ─────────────────────────────────────────────────

func TestCollectExpansions_RecordsEveryInvocation(t *testing.T) {
	src := `
@macro hi() { runtime.const @{ value: "hi" } }
@hi() -> @hi()
`
	exps, err := collectExpansionsWithContext(context.Background(), expandCfg(t), src, "test.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireLen(t, exps, 2)
	ktest.RequireEqual(t, exps[0].Macro, "hi")
	ktest.RequireEqual(t, exps[1].Macro, "hi")
}

func TestCollectExpansions_NoMacros(t *testing.T) {
	exps, err := collectExpansionsWithContext(context.Background(), expandCfg(t), `noop`, "test.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireLen(t, exps, 0)
}

func TestCollectExpansions_InsidePipeline(t *testing.T) {
	src := `
@macro hi() { runtime.const @{ value: "hi" } }
@pipeline p
  @hi()
@end
`
	exps, err := collectExpansionsWithContext(context.Background(), expandCfg(t), src, "test.nflow")
	ktest.RequireNoError(t, err)
	ktest.RequireLen(t, exps, 1)
	ktest.RequireEqual(t, exps[0].Macro, "hi")
	ktest.RequireEqual(t, exps[0].Where, "pipeline p")
}

func TestCollectExpansions_ParseFailureStillReturnsRecorded(t *testing.T) {
	// `nflow expand` on a broken file is still useful: the recorded
	// body plus the parse error together name the failing token. The
	// error is returned, and the trace is populated despite it.
	src := `
@macro broken() { noop -> ) }
@broken()
`
	exps, err := collectExpansionsWithContext(context.Background(), expandCfg(t), src, "test.nflow")
	ktest.RequireNotNil(t, err)
	ktest.RequireLen(t, exps, 1)
	ktest.RequireEqual(t, exps[0].Macro, "broken")
	ktest.RequireNotNil(t, exps[0].Err)
}

// ── renderExpansions ──────────────────────────────────────────────────

func TestRenderExpansions_EmitsMacroAndBody(t *testing.T) {
	var buf bytes.Buffer
	exps := []macros.Expansion{{
		Macro:      "hi",
		DefLine:    2,
		InvokeLine: 5,
		Where:      "top-level",
		Body:       `runtime.const @{ value: "hi" }`,
	}}

	renderExpansions(&buf, exps, "test.nflow", nil)

	out := buf.String()
	ktest.RequireStringContains(t, out, "@hi")
	ktest.RequireStringContains(t, out, `runtime.const @{ value: "hi" }`)
	ktest.RequireStringContains(t, out, "line 2")
	ktest.RequireStringContains(t, out, "line 5")
	ktest.RequireStringContains(t, out, "top-level")
}

func TestRenderExpansions_FilterKeepsMatchingMacro(t *testing.T) {
	var buf bytes.Buffer
	exps := []macros.Expansion{
		{Macro: "keep", DefLine: 2, InvokeLine: 5, Where: "top-level", Body: "a"},
		{Macro: "drop", DefLine: 3, InvokeLine: 6, Where: "top-level", Body: "b"},
	}

	renderExpansions(&buf, exps, "test.nflow", []string{"keep"})

	out := buf.String()
	ktest.RequireStringContains(t, out, "@keep")
	ktest.RequireStringNotContains(t, out, "@drop")
}

func TestRenderExpansions_FilterMatchesNothing(t *testing.T) {
	var buf bytes.Buffer
	exps := []macros.Expansion{
		{Macro: "only", DefLine: 2, InvokeLine: 5, Where: "top-level", Body: "a"},
	}

	renderExpansions(&buf, exps, "test.nflow", []string{"missing"})

	ktest.RequireStringContains(t, buf.String(), "no expansions matched")
}

func TestRenderExpansions_ErrorIsRendered(t *testing.T) {
	var buf bytes.Buffer
	exps := []macros.Expansion{{
		Macro:      "bad",
		DefLine:    2,
		InvokeLine: 5,
		Where:      "top-level",
		Body:       "x",
		Err:        errors.New("unexpected token )"),
	}}

	renderExpansions(&buf, exps, "test.nflow", nil)

	ktest.RequireStringContains(t, buf.String(), "unexpected token )")
}

// expandCfg returns the shipped bundle set. macros is native now:
// native.Bundles() already contains it, and appending it again would
// produce a duplicate ID error at BuildConfig.
func expandCfg(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)
	return cfg
}
