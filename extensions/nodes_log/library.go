// Package nodes_log provides log.info, log.warn, and log.error as
// pass-through nodes. Each emits a structured slog line and returns
// the input unchanged, so a .nflow pipeline can insert observability
// between steps without leaving the pipeline.
//
// Typical use:
//
//	{ value: "starting" } -> log.info -> actual_work
package nodes_log

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "nodes_log"

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
			LogInfo,
			LogWarn,
			LogError,
		},
	}
}
