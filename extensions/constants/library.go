package constants

import (
	"embed"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

const ID = "constants"

//go:embed nflows
var fixturesFS embed.FS

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
		Fixtures:   fixturesFS,
	}
}
