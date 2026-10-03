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
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "nodes_bench"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		ArgSchemas: map[string][]core.ArgFieldSpec{
			"bench.run": {{Name: "action", Kind: core.ArgCapabilityRef}},
		},
		SelfTest: selftest,
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
