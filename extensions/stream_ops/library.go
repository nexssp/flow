// Package stream_ops exposes kernel stream operations to Flow DSL.
//
// Typical use:
//
//	fs.walk -> stream.batch @{ size: 100 } -> process
package stream_ops

import (
	"embed"

	"github.com/nexssp/kernel/action"

	core "github.com/nexssp/flow/compiler"
)

const ID = "stream_ops"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name:      ID,
		Operators: Operators(),
	}
}
