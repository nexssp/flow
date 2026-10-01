// Package fs ships the file-system stream source and operators:
// fs.walk as a source; fs.filter, fs.read, fs.sort, fs.write, and
// the terminal sinks out.stdout and out.file as stream operators.
//
// The pipeline carries fs.FileMeta between stages; fs.read replaces it
// with fs.FileContent (metadata + bytes) so downstream stages can
// inspect and transform file bodies.
//
// Typical use:
//
//	fs.walk:ext="go" -> fs.filter:tests=true -> fs.read -> fs.sort:by="size" -> collect
package fs

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "fs"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		SelfTest:  selftest,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Sources: []action.AnyStreamAction{
			WalkSource(),
		},
		Operators: []action.NamedOperator{
			FilterOperator(),
			ReadOperator(),
			WriteOperator(),
			SortOperator(),
			StdoutOperator(),
			OutFileOperator(),
		},
		Aliases: []action.Alias{
			{Canonical: "fs.filter", Short: []string{"stream.filter", "filter"}},
			{Canonical: "fs.read", Short: []string{"stream.read", "read"}},
			{Canonical: "fs.sort", Short: []string{"sort"}},
			{Canonical: "fs.write", Short: []string{"stream.write"}},
			{Canonical: "out.stdout", Short: []string{"stdout"}},
			{Canonical: "out.file", Short: []string{"write_file", "save"}},
		},
	}
}
