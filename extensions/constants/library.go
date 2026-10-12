package constants

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "constants"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{ConstDirective.Action(), ConstLoadDirective.Action()},
		}},
		Directives: []compiler.Directive{ConstDirective, ConstLoadDirective},
		Fixtures:   fixturesFS,
	}
}
