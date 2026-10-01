// Package nodes_distribute provides distribute.map (bounded-concurrency
// fan-out over a slice) and distribute.reduce (fold the map output into
// a single value).
//
// Typical use:
//
//	{ action: "log.info", items: [1, 2, 3], concurrency: 4 } -> distribute.map
//	{ strategy: "all_pass", items: .results } -> distribute.reduce
package nodes_distribute

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "nodes_distribute"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			DistributeMap,
			DistributeReduce,
		},
		Aliases: []action.Alias{
			{Canonical: "distribute.map", Short: []string{"map", "fanout", "parallel"}},
			{Canonical: "distribute.reduce", Short: []string{"reduce", "fold"}},
		},
	}
}

// Item is one entry in a distribute.map result. Exactly one of Result
// or Error is populated per item.
type Item struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}
