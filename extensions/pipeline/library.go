// Package pipeline provides the `@pipeline NAME ... @end` directive.
// The block is stored in meta["pipelines"] and compiled into a
// sub-action mounted on the resolver by the Materialize step, so
// later pipelines can reference it by name.
//
// Typical use:
//
//	@pipeline safe_fetch
//	  fetch -> validate
//	@end
//
//	{ url: "https://x" } -> safe_fetch
package pipeline

import (
	"embed"
	"maps"
	"slices"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "pipeline"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:          ID,
		Libraries:   []action.Library{{Name: ID}},
		Directives:  []compiler.Directive{Directive, EndDirective},
		Materialize: materialize,
		SelfTest: func() []compiler.SelfTestSection {
			return []compiler.SelfTestSection{{
				Name: ID,
				Features: []compiler.SelfTestFeature{{
					Name:   "response schema defaults are rejected before execution",
					Source: "pipeline/selftest/response-schema-default",
					DSL: `@schema Response {
						  Value string ` + "`json:\"value\" default:\"\"`" + `
						}
						@pipeline invalid
						  :response-schema=Response
						  runtime.noop
						@end
						`,
					ExpectCompileError: "response schemas must not declare defaults",
				}},
			}}
		},
		MaterializePhase: compiler.MaterializeCompositions,
		Fixtures:         fixturesFS,
		Build: compiler.BuildExtension{
			// Keep preprocessed pipeline bodies explicit: raw-source lexing can
			// miss their dependencies after a colon in a quoted @description
			// (scanAtPrompt/scanString). The @{...} quote fix does not fix that.
			Sources: func(meta map[string]any) []string {
				pipelines, _ := meta["pipelines"].(map[string]string)
				var sources []string
				for _, name := range slices.Sorted(maps.Keys(pipelines)) {
					sources = append(sources, pipelines[name])
				}
				return sources
			},
		},
	}
}
