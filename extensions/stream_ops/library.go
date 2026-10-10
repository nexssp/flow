// Package stream_ops exposes kernel stream operations to Flow DSL.
//
// Typical use:
//
//	fs.walk -> stream.batch @{ size: 100 } -> process
package stream_ops

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "stream_ops"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
	}
}

func Library() action.Library {
	return action.Library{
		Name:      ID,
		Operators: Operators(),
	}
}
