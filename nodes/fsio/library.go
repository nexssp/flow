package fsio

import (
	"github.com/nexssp/kernel/action"
)

func Library() action.Library {
	return action.Library{
		Name: "flow/fsio",
		Sources: []action.AnyStreamAction{
			WalkSource(),
		},
		Operators: []action.NamedOperator{
			FilterOperator(),
			ReadOperator(),
			SortOperator(),
			StdoutOperator(),
			OutFileOperator(),
			ClipboardOperator(),
		},
		Aliases: []action.Alias{
			{Canonical: "fs.filter", Short: []string{"stream.filter", "filter"}},
			{Canonical: "fs.read", Short: []string{"stream.read", "read"}},
			{Canonical: "fs.sort", Short: []string{"sort"}},
			{Canonical: "out.stdout", Short: []string{"stdout"}},
			{Canonical: "out.file", Short: []string{"fs.write_file", "write"}},
			{Canonical: "clipboard.write", Short: []string{"copy"}},
		},
	}
}

func All() ([]action.AnyStreamAction, []action.NamedOperator) {
	return []action.AnyStreamAction{WalkSource()},
		[]action.NamedOperator{
			FilterOperator(),
			ReadOperator(),
			SortOperator(),
			StdoutOperator(),
			OutFileOperator(),
			ClipboardOperator(),
		}
}
