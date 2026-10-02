package match

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "match"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		Primaries: []core.PrimaryExtension{matchKeyword{}},
		SelfTest:  selftest,
	}
}
