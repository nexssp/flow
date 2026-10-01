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

	"github.com/nexssp/flow/core"
)

const ID = "pipeline"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:          ID,
		Libraries:   []action.Library{{Name: ID}},
		Directives:  []core.Directive{Directive},
		Materialize: materialize,
		Fixtures:    fixturesFS,
	}
}
