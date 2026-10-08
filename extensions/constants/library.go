package constants

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "constants"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{ConstDirective.Action(), ConstLoadDirective.Action()},
		}},
		Directives: []core.Directive{ConstDirective, ConstLoadDirective},
	}
}
