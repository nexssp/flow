// Package decide provides the `decide` action: it evaluates a state
// against a registered decision backend and returns structured answers.
// Backends are registered by the application before the pipeline runs;
// the .nflow file only names the backend.
//
// Typical use:
//
//	{ backend: "http-judge", state: .state, questions: { verdict: { type: "label", labels: ["ok","bad"] } } } -> decide
package decide

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "decide"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{DecideAction}}},
		SelfTest:  selftest,
	}
}
