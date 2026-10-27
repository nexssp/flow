package lang

import (
	"context"

	"github.com/nexssp/flow/compiler"
)

// preflightValidator is the context-aware AST validator contributed via
// OnPreprocess. It runs after parse, before Analyze/Build — the "no run if
// anything is missing" gate.
//
// The validator walks the parsed AST, collects every *langBlockExpr, extracts
// their LangBlock IDs/versions, and runs Preflight.Run. If any interpreter
// is missing, the Report renders as an error and the flow does not build.
//
// Preflight uses Select first. Production compilation may invoke the configured
// installer for a missing interpreter; self-test contexts opt out so missing
// runtimes are reported immediately rather than installed.
func preflightValidator(ctx context.Context, ast compiler.Expr, _ map[string]any) error {
	var blocks []*LangBlock
	collectLangBlocks(ast, &blocks)
	if len(blocks) == 0 {
		return nil // no @lang tags in this flow — nothing to check
	}
	var installer Ensure
	if !autoInstallDisabled(ctx) {
		installer = ensureHook
	}
	report := Preflight{Installer: installer}.Run(blocks)
	if report.OK() {
		return nil
	}
	return report // Report implements error
}

// collectLangBlocks walks the AST depth-first, collecting every
// *langBlockExpr into the slice. Uses compiler.AtomNames' walkExpr
// pattern (depth-first, Children() recursion).
func collectLangBlocks(e compiler.Expr, out *[]*LangBlock) {
	if e == nil {
		return
	}
	if lb, ok := e.(*langBlockExpr); ok {
		*out = append(*out, lb.block)
	}
	for _, child := range e.Children() {
		collectLangBlocks(child, out)
	}
}
