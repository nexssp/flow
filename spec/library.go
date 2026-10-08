// Package spec osadza oficjalne pliki specyfikacji .nflow.
// Każdy plik w tym folderze jest automatycznie testowany przez 'nflow self test'.
package spec

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "spec"

//go:embed *.nflow
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Fixtures:  fixturesFS,
	}
}
