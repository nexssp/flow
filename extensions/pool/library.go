// Package pool provides the `@pool NAME [member1, member2] { strategy:
// "round_robin" }` directive. Each declaration is materialized as a
// callable `pool.<name>` action.
//
// Typical use:
//
//	@pool workers [const, noop] { strategy: "failover" }
//	pool.workers @{ value: "x" }
package pool

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "pool"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:          ID,
		Libraries:   []action.Library{{Name: ID}},
		Directives:  []compiler.Directive{Directive},
		Materialize: materialize,
		Fixtures:    fixturesFS,
	}
}
