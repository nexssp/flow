// Package io ships process-stdio stream primitives: io.stdin reads
// newline-delimited items from standard input; io.stdout and io.stderr
// write each item as a line and pass it through. Neither sink owns the
// stream — the CLI still writes the final payload to stdout.
package io

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "io"

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
