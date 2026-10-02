// Package nodes_supervisor provides the supervisor action: it compiles
// and runs a set of named child pipelines concurrently, isolating each
// child's panic, error, and timeout.
//
// Typical use:
//
//	{ tasks: [
//	    { id: "a", dsl: "noop", payload: { x: 1 } },
//	    { id: "b", dsl: "log.info", timeout_ms: 500 }
//	]} -> supervisor.run
package nodes_supervisor

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "nodes_supervisor"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{Supervisor}}},
		SelfTest:  selftest,
	}
}
