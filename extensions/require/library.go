package require

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "require"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{Directive.Action()},
		}},
		Directives: []core.Directive{Directive},
	}
}
