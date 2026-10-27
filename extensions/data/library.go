// Package data provides JSONL datasets and deterministic stream operations.
//
// Typical use:
//
// @data examples :from="examples.jsonl"
// @datause examples
// -> data.split:partition="train" :key="id" :train=80 :validation=10 :test=10 :seed=42
// -> data.where:expression="is_positive == true"
// -> data.select:fields="id,text,label"
// -> collect
package data

import (
	"embed"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "data"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:           ID,
		Libraries:    []action.Library{Library()},
		Directives:   []compiler.Directive{DataDirective, DataUseDirective},
		InheritScope: inheritDatasetScope,
		WrapPipeline: wrapPipeline,
		Fixtures:     fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name:    ID,
		Sources: []action.AnyStreamAction{RowsSource()},
		Operators: []action.NamedOperator{
			WhereOperator(),
			SelectOperator(),
			SplitOperator(),
		},
	}
}
