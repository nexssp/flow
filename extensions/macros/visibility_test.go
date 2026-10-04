package macros_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

// cfg is the minimal bundle set every test in this file runs against.
// It is exactly what a user would load with three @require lines, plus
// macros. Building it once per test keeps the bundle set explicit.
func cfg(t *testing.T) runner.Config {
	t.Helper()
	c, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		pipeline.Bundle(nil),
		macros.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return c
}

// ── HANDOVER §5.2 — unknown-macro error message ───────────────────────

func TestUnknownMacro_NoDeclarations_ErrorNamesMacro(t *testing.T) {
	// The exact case that motivated MergeablePrimary. Before the fix,
	// the macro primary was never installed on a source with zero
	// declarations, so the parser reported "unexpected token". The
	// primary is now installed unconditionally; the message names the
	// missing macro instead.
	_, err := runner.Execute(context.Background(), cfg(t), `@does_not_exist`, "test.nflow", nil)
	ktest.RequireErrorContains(t, err, "unknown macro @does_not_exist")
}

func TestUnknownMacro_WithOtherDeclarations_ErrorNamesMissingMacro(t *testing.T) {
	// Regression guard. A file that declares one macro and references a
	// second, undeclared one must keep producing the specific error.
	src := `
@macro declared() { runtime.noop }
@undeclared()
`
	_, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireErrorContains(t, err, "unknown macro @undeclared")
}

func TestUnknownMacro_ErrorIsNotUnexpectedToken(t *testing.T) {
	// The old failure mode was indistinguishable from a generic parse
	// error. Pin the exact wording so it cannot silently regress to the
	// parser fallback while still "producing an error".
	_, err := runner.Execute(context.Background(), cfg(t), `@missing`, "test.nflow", nil)
	ktest.RequireErrorNotContains(t, err, "unexpected token")
}

// ── sub-source visibility ─────────────────────────────────────────────

func TestSubPipeline_SeesParentMacro(t *testing.T) {
	src := `
@macro hi() { runtime.const @{ value: "hi" } }
@pipeline p
  @hi()
@end
pipeline.p
`
	ex, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "hi")
}

func TestSubPipeline_DeclaresOwnMacro(t *testing.T) {
	// The sub-source contributes its own primary (macros declared inside
	// the pipeline body). With the mergeable contract, the sub-source's
	// primary combines with the parent's empty one instead of panicking.
	src := `
@pipeline p
  @macro local() { runtime.const @{ value: "local" } }
  @local()
@end
pipeline.p
`
	ex, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "local")
}

func TestSubPipeline_ParentAndLocalMacrosBothVisible(t *testing.T) {
	src := `
@macro parent() { runtime.const @{ value: "parent" } }
@pipeline p
  @macro local() { runtime.const @{ value: "local" } }
  @parent()
@end
pipeline.p
`
	ex, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "parent")
}

func TestSubPipeline_LocalMacroShadowsParent(t *testing.T) {
	// Both declarations are named `pick`. MergeWith keeps the newer
	// (sub-source) declaration on collision, so the body resolves to
	// the local one.
	src := `
@macro pick() { runtime.const @{ value: "parent" } }
@pipeline p
  @macro pick() { runtime.const @{ value: "local" } }
  @pick()
@end
pipeline.p
`
	ex, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "local")
}

func TestSubPipeline_UnknownMacroInBody_ErrorNamesMacro(t *testing.T) {
	src := `
@pipeline p
  @does_not_exist()
@end
pipeline.p
`
	_, err := runner.Execute(context.Background(), cfg(t), src, "test.nflow", nil)
	ktest.RequireErrorContains(t, err, "unknown macro @does_not_exist")
}
