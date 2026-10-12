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

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "nodes_dispatch"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{Dispatch}}},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"dispatch.run": {
				{Name: "members", Kind: compiler.ArgCapabilityRefList},
			},
		},
		Fixtures: fixturesFS,
	}
}
