// Package pool provides the `@pool NAME [member1, member2] { strategy:
// "round_robin" }` directive. Each declaration is mounted as a
// `pool.<name>` action whose behavior depends on the strategy, and is
// also published through the execution context so `dispatch` can
// resolve it by name.
//
// Typical use:
//
//	@pool workers [const, noop] { strategy: "failover" }
//	{ pool: "workers", payload: { value: "x" } } -> dispatch
package pool

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "pool"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:           ID,
		Libraries:    []action.Library{{Name: ID}},
		Directives:   []core.Directive{Directive},
		Materialize:  materialize,
		WrapPipeline: wrapWithPools,
		Fixtures:     fixturesFS,
	}
}
