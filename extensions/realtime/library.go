// Package realtime provides soft real-time primitives: fixed-timestep
// loop generators, timing wheels, and lock-free topics.
package realtime

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "realtime"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Sources: []action.AnyStreamAction{
			TickerSource(),
		},
		Actions: []action.AnyAction{
			TopicPublishAction(),
			WheelScheduleAction(),
			WheelCancelAction(),
			WheelAdvanceAction(),
		},
	}
}
