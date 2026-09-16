package flow

import (
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func StandardLibrary() action.Library {
	return action.Library{
		Name:        "flow",
		Description: "Standard flow actions: log, bench, distribute, supervisor",
		Actions:     nodes.All(),
		Aliases: []action.Alias{
			{Canonical: "log.info", Short: []string{"log", "info"}},
			{Canonical: "log.warn", Short: []string{"warn"}},
			{Canonical: "log.error", Short: []string{"error"}},
			{Canonical: "bench.run", Short: []string{"bench", "benchmark"}},
			{Canonical: "bench.save", Short: []string{"save_bench"}},
			{Canonical: "bench.compare", Short: []string{"compare", "diff"}},
			{Canonical: "distribute.map", Short: []string{"map", "fanout", "parallel"}},
			{Canonical: "distribute.reduce", Short: []string{"reduce", "fold"}},
		},
	}
}
