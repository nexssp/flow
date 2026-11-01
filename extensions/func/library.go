// Package funcs implements source-local @func declarations. Function bodies are
// pure expressions, callable from projections, conditions, and argument maps.
package funcs

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/projection"
)

const ID = "func"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle contributes the @func directive, expression compiler, and projection
// primary for each prepared source. No function definitions are placed in a
// process-global registry.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:           ID,
		Libraries:    []action.Library{{Name: ID}},
		Directives:   []compiler.Directive{Directive},
		InheritScope: inheritFunctions,
		OnPreprocess: func(meta map[string]any) compiler.PreprocessContributions {
			set := functionSetFromMeta(meta)
			if set == nil {
				return compiler.PreprocessContributions{}
			}
			compiler.SetExpressionCompiler(meta, set)
			return compiler.PreprocessContributions{
				Primaries: []compiler.PrimaryExtension{projection.PrimaryWithFunctions(set)},
				CompileOpts: []compiler.CompileOption{
					compiler.WithExpressionCompiler(set),
					compiler.WithASTValidators(func(_ compiler.Expr, _ map[string]any) error {
						return set.Validate()
					}),
				},
			}
		},
		Materialize: func(req compiler.MaterializeReq) error {
			return functionSetFromMeta(req.Meta).Validate()
		},
		Fixtures: fixturesFS,
	}
}

// ExpressionCompilerFromMeta returns a compiler for all functions visible in
// one source, including functions mounted through its @include namespaces.
func ExpressionCompilerFromMeta(meta map[string]any) compiler.ExpressionCompiler {
	set := functionSetFromMeta(meta)
	if set == nil {
		return nil
	}
	return set
}
