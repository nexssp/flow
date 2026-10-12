// Package template provides a reusable Go text-template renderer and exposes
// it to Flow as the template.render action.
//
// Typical use:
//
//	template.render @{ template: "Hello {{.name}}", variables: { name: "Flow" } }
package template

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "template"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle exposes the template.render action to Flow.
func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

// Library returns the action library without requiring registry lookup.
func Library() action.Library {
	return action.Library{
		Name:    ID,
		Actions: []action.AnyAction{RenderAction},
	}
}
