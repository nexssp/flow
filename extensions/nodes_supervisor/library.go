// Package nodes_supervisor provides finite child-pipeline fan-out through
// supervisor.run and long-lived service orchestration through supervisor.serve.
//
// Finite fan-out:
//
//	{ tasks: [
//	    { id: "a", dsl: "noop", payload: { x: 1 } },
//	    { id: "b", dsl: "log.info", timeout_ms: 500 }
//	]} -> supervisor.run
//
// Long-lived services are declared directly in NFlow and signal readiness
// through compiler/contracts when transport-specific startup completes.
package nodes_supervisor

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "nodes_supervisor"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID, Actions: []action.AnyAction{Supervisor, Serve}}},
		Fixtures:   fixturesFS,
		AtomAdvise: adviseServe,
	}
}
