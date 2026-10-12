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
		Fixtures:    fixturesFS,
	}
}
