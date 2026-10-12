// Package nodes_bench provides bench.run (measure latency distribution
// of an action), bench.save (write results to a file), and
// bench.compare (diff against a stored baseline).
//
// Typical use:
//
//	{ action: "log.info", iterations: 100, payload: { message: "x" } } -> bench.run
//	{ file: "bench/baseline.json" } -> bench.save
//	{ baseline: "bench/baseline.json", tolerance_pct: 5 } -> bench.compare
package nodes_bench

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "nodes_bench"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		ArgSchemas: map[string][]compiler.ArgFieldSpec{
			"bench.run": {{Name: "action", Kind: compiler.ArgCapabilityRef}},
		},
		Fixtures: fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			BenchRun,
			BenchSave,
			BenchCompare,
		},
	}
}
