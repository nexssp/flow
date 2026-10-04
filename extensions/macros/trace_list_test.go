package macros_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/macros"
)

func TestTrace_RecordsTopLevelExpansion(t *testing.T) {
	// Source has 6 lines; @macro is on line 2, @hi() on line 5.
	exps := parseWithTrace(t, `
@macro hi() {
  noop
}
@hi()
`)
	ktest.RequireLen(t, exps, 1)
	ktest.RequireEqual(t, exps[0].Macro, "hi")
	ktest.RequireEqual(t, exps[0].DefLine, 2)
	ktest.RequireEqual(t, exps[0].InvokeLine, 5)
	ktest.RequireEqual(t, exps[0].Where, "top-level")
	ktest.RequireNil(t, exps[0].Err)
	ktest.RequireStringContains(t, exps[0].Body, "noop")
}

func TestTrace_RecordsFailure(t *testing.T) {
	// The recorded expansion carries the sub-parse error even when the
	// overall parse fails; that is what makes `nflow expand` useful on
	// a broken file.
	exps := parseWithTrace(t, `
@macro broken() {
  noop -> )
}
@broken()
`)
	ktest.RequireLen(t, exps, 1)
	ktest.RequireEqual(t, exps[0].Macro, "broken")
	ktest.RequireNotNil(t, exps[0].Err)
}

func TestTrace_NoMacroNoRecord(t *testing.T) {
	ktest.RequireLen(t, parseWithTrace(t, `noop`), 0)
}

func TestTrace_RecordsNestedExpansion(t *testing.T) {
	// The inner expansion finishes first because SubParse completes
	// before the outer record is written.
	exps := parseWithTrace(t, `
@macro inner() { noop }
@macro outer() { @inner() }
@outer()
`)
	ktest.RequireLen(t, exps, 2)
	ktest.RequireEqual(t, exps[0].Macro, "inner")
	ktest.RequireEqual(t, exps[1].Macro, "outer")
}

// parseWithTrace preprocesses src through the macro directive table so
// @macro declarations land in meta, installs the primary the bundle
// would build, parses with a trace attached, and returns the recorded
// expansions. The parse error is deliberately not returned: the
// failure-path test needs the trace, and the success-path tests would
// only re-assert what RequireNoError already covers.
func parseWithTrace(tb testing.TB, src string) []macros.Expansion {
	tb.Helper()

	trace := &macros.Trace{}
	ctx := macros.WithTrace(context.Background(), trace)
	ctx = macros.WithWhere(ctx, "top-level")

	dt := core.NewDirectiveTable(macros.Directive)
	clean, meta, err := core.Preprocess(ctx, dt, src, "test.nflow")
	ktest.RequireNoError(tb, err)

	contribs := macros.Bundle(nil).OnPreprocess(meta)
	primaries := core.NewPrimaryExtensionTable(contribs.Primaries...)

	// Parse the preprocessed source, not the raw source. @macro
	// declarations are directives and have been stripped into meta;
	// only the cleaned body contains the @name invocations the primary
	// is meant to dispatch.
	_, _ = core.NewParserWithFileOffset(
		ctx, core.NewOperatorTable(), primaries, clean, "test.nflow", 0,
	).Parse()

	return trace.Expansions()
}
