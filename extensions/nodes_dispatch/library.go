// Package nodes_dispatch provides the `dispatch` action, which selects
// one action from a named pool (or explicit member list) and invokes
// it with the supplied payload. On failure it falls through the
// remaining members.
//
// Typical use:
//
//	{ members: "log.info,noop", payload: { value: "x" } } -> dispatch
//	{ pool: "workers", chosen: "log.info", payload: .data } -> dispatch
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
		Fixtures:  fixturesFS,
	}
}
