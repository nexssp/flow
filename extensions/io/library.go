// Package io ships process-stdio stream primitives: io.stdin reads
// newline-delimited items from standard input; io.stdout and io.stderr
// write each item as a line and pass it through. Neither sink owns the
// stream — the CLI still writes the final payload to stdout.
package io

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "io"

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
		Name: ID,
		Sources: []action.AnyStreamAction{
			InSource(),
		},
		Operators: []action.NamedOperator{
			StdoutOperator(),
			StderrOperator(),
		},
	}
}
