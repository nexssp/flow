package match

import (
	"embed"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "match"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Primaries: []core.PrimaryExtension{matchKeyword{}},
		Fixtures:  fixturesFS,
	}
}
