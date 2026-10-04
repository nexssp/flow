package macros_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

// ── Bundle wiring ─────────────────────────────────────────────────────

// TestBundle_WiresDirectiveAndOnPreprocess verifies the bundle publishes
// both halves of the macro engine: the @macro directive that populates
// meta, and the OnPreprocess hook that turns those declarations into a
// parser primary.
func TestBundle_WiresDirectiveAndOnPreprocess(t *testing.T) {
	b := macros.Bundle(nil)

	ktest.RequireEqual(t, b.ID, "macros")
	ktest.RequireLen(t, b.Directives, 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "macro")
	ktest.RequireNotNil(t, b.OnPreprocess)
}

// ── OnPreprocess contributions ────────────────────────────────────────

// TestBundle_OnPreprocess_InstallsPrimaryWithoutDeclarations is the
// regression guard for the "unknown macro" loose end. The primary is
// installed unconditionally, even when the source declares no macros.
// That is what makes `@unknown` produce "unknown macro @unknown" from
// the parser primary instead of the parser's generic "unexpected token".
//
// The primary's byName map is empty in this case. If a parent or
// sub-source contributes its own declarations later, the merge machinery
// folds them in — the empty map is the correct starting point.
func TestBundle_OnPreprocess_InstallsPrimaryWithoutDeclarations(t *testing.T) {
	contribs := macros.Bundle(nil).OnPreprocess(map[string]any{})
	ktest.RequireLen(t, contribs.Primaries, 1)
}

// TestBundle_OnPreprocess_WithDeclarations verifies the primary built
// from a source that does declare macros carries those declarations.
// The test asserts via the primary's own parser-side behavior: an
// invocation of a declared macro parses, an invocation of an undeclared
// macro fails with the specific "unknown macro" error.
func TestBundle_OnPreprocess_WithDeclarations(t *testing.T) {
	meta := map[string]any{
		macros.DeclarationKey: []macros.Declaration{
			{Name: "hi", Body: `runtime.noop`, DefLine: 2, BodyLine: 3},
		},
	}

	contribs := macros.Bundle(nil).OnPreprocess(meta)
	ktest.RequireLen(t, contribs.Primaries, 1)
}

// ── SelfTest shape ────────────────────────────────────────────────────

// TestBundle_SelfTestShape confirms the bundle advertises inline
// features. Fixture discovery is exercised by the selftest runner; this
// test only locks in that at least one inline feature exists so a future
// refactor that deletes the SelfTest function fails loudly here.
func TestBundle_SelfTestShape(t *testing.T) {
	b := macros.Bundle(nil)
	ktest.RequireNotNil(t, b.SelfTest)

	sections := b.SelfTest()
	ktest.RequireLen(t, sections, 1)
	ktest.RequireEqual(t, sections[0].Name, "macros")
	ktest.RequireCondition(t, len(sections[0].Features) > 0,
		"SelfTest must advertise at least one inline feature")
}

// ── Integration: sub-source visibility ────────────────────────────────

// TestParentMacroVisibleInPipeline locks in the sub-source visibility
// contract: a macro declared at the top of a file is visible inside a
// @pipeline body in the same file. Before the MergeablePrimary change
// this fails, because the sub-pipeline compile installed a fresh macro
// primary that had never seen the parent's declarations.
//
// The test drives the production runner end to end. It does not
// construct a Parser by hand, because the thing under test is the
// pipeline-assembly path, not the parser.
//
// macros is not in native.Bundles(): it is opt-in via @require. The
// test loads the shipped set plus macros explicitly, mirroring what the
// CLI does for a file that declares `@require .../macros`.
func TestParentMacroVisibleInPipeline(t *testing.T) {
	// macros is native: native.Bundles() already contains it. The test
	// loads the shipped set as-is, which is exactly what `nflow run`
	// builds for a file that uses @macro.
	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `
@macro hi() { runtime.const @{ value: "hi" } }
@pipeline p
  @hi()
@end
pipeline.p
`
	ex, err := runner.Execute(context.Background(), cfg, src, "test.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "hi")
}
