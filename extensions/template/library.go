// Package template provides a reusable Go text-template renderer and exposes
// it to Flow as the template.render action.
//
// Typical use:
//
//	template.render @{ template: "Hello {{.name}}", variables: { name: "Flow" } }
package template

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "template"

func init() {
	core.Register(ID, Bundle)
}

// Bundle exposes the template.render action to Flow.
func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
	}
}

// Library returns the action library without requiring registry lookup.
func Library() action.Library {
	return action.Library{
		Name:    ID,
		Actions: []action.AnyAction{RenderAction},
	}
}
