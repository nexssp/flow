// Package bytes ships explicit byte-slice operations for Flow.
//
// bytes.concat joins []byte chunks without separators or text conversion and
// returns the exact bytes in a Go string for text-oriented output boundaries.
// The Go string preserves arbitrary bytes in memory, but JSON serialization is
// not a byte-safe transport for invalid UTF-8; keep it on the output path.
package bytes

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "bytes"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		Fixtures:  fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name:    ID,
		Actions: []action.AnyAction{ConcatAction()},
	}
}
