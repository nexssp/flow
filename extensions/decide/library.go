// Package decide provides the `decide` action: it evaluates a state
// against a registered decision backend and returns structured answers.
// Backends are registered by the application before the pipeline runs;
// the .nflow file only names the backend.
//
// Typical use:
//
//	{ backend: "http-judge", state: .state, questions: { verdict: { type: "label", labels: ["ok","bad"] } } } -> decide.run
package decide

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "decide"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID, Actions: []action.AnyAction{DecideAction}}},
		Fixtures:  fixturesFS,
	}
}
