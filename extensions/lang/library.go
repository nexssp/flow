// Package lang ships inline polyglot execution through the `lang` bundle,
// using directives and bare keyword aliases such as py, rs, go, and ts.
//
// Each tag compiles to a single LangBlock AST node. The runtime materializes
// the source to a temp file, resolves the interpreter, and delegates execution
// to extensions/external via the executor seam (see exec.go). No new
// subprocess engine lives in this package.
//
// Typical use:
//
//	@require lang
//	{ "x": 2, "y": 3 } -> py"import json, sys; v=json.load(sys.stdin); print(json.dumps({'value': v['x'] + v['y']}))"
//
//	py {
//	    import sys, json
//	    data = json.load(sys.stdin)
//	    print(json.dumps({"len": len(data)}))
//	}
//
// The bundle is registered for @require but is not mounted in the default
// native bundle set. Add @require lang to a flow that wants the keywords.
package lang

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

// ID is the bundle identifier. Short, stable, never an import path.
// Two extensions with the same ID panic at compiler.Register.
const ID = "lang"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	// Load runner definitions from embedded runners/*.nflow before
	// registering the bundle. Each runner file declares a Language
	// (and its aliases) as data — zero Go code per language. If a runner
	// file is malformed, fail loudly here rather than at first use.
	if err := LoadRunners(); err != nil {
		panic("lang: " + err.Error())
	}
	// Swap the executor seam from the standalone default to the
	// production adapter that delegates to external.Exec. This is the
	// only place the swap happens — exec.go's defaultExecutor remains
	// the fallback for standalone tests.
	execRunner = externalExecAdapter{}
	compiler.Register(ID, Bundle)
}

// Bundle wires the language tag keywords, the @lang.runner and
// @lang.define directives, and the embedded runner source modules.
//
// It returns the contract documented in extensions/README.md:
// ID, Libraries, Directives, Primaries, Fixtures, SourceModules.
//
//   - Primaries: contributed by OnPreprocess from the registered aliases.
//     Adding an alias to a runner file makes it available without a Go edit.
//   - Directives: @lang.runner and @lang.define for inline declarations.
//     See lang_directive.go.
//   - SourceModules: embedded runners/*.nflow, mirroring the nexssp/ai
//     pattern. Consumers can @include runner files as data.
//
// Feature logic lives in sibling files; this file does only wiring.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Directives: []compiler.Directive{
			RunnerDirective,
			DefineDirective,
		},
		Fixtures:      fixturesFS,
		SourceModules: runnersFS,
		ExportedModules: []string{
			"runners/go.nflow",
			"runners/javascript.nflow",
			"runners/python.nflow",
			"runners/python2.nflow",
			"runners/python3.nflow",
			"runners/rust.nflow",
			"runners/shell.nflow",
			"runners/typescript.nflow",
		},
		OnPreprocess: func(_ map[string]any) compiler.PreprocessContributions {
			return compiler.PreprocessContributions{
				Primaries: keywordPrimaries(),
				CompileOpts: []compiler.CompileOption{
					compiler.WithASTContextValidators(preflightValidator),
				},
			}
		},
	}
}
