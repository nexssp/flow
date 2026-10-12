package match

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

//go:embed nflows
var fixturesFS embed.FS

const ID = "match"

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Primaries: []compiler.PrimaryExtension{matchKeyword{}},
		Fixtures:  fixturesFS,
	}
}
