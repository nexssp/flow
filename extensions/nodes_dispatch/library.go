// Package nodes_dispatch provides the `dispatch.run` action, which tries
// explicitly listed actions in order and returns the first successful result.
//
// Typical use:
//
//	dispatch.run @{ members: [runtime.fail, runtime.const], payload: { value: "x" } }
package nodes_dispatch

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "nodes_dispatch"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{Dispatch}}},
		ArgSchemas: map[string][]core.ArgFieldSpec{
			"dispatch.run": {
				{Name: "members", Kind: core.ArgCapabilityRefList},
			},
		},
		Fixtures: fixturesFS,
	}
}
