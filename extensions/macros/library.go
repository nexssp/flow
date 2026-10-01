// Package macros extends the compiler with a @macro engine.
//
// A @macro directive stores a declaration; OnPreprocess turns each
// declaration into a PrimaryExtension dispatching on TokAtPrompt; and
// every expansion re-enters the parser with the caller's arguments
// substituted into the body.
//
// Typical use:
//
//	@macro greet(name) { const @{ value: $name } }
//	@greet("World")   // expands to: const @{ value: "World" }
//
// Without this bundle, @name is "unexpected token @name".
package macros

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "macros"

// DeclarationKey is the meta slot where @macro declarations land before
// OnPreprocess converts them to a PrimaryExtension.
const DeclarationKey = "macros"

// Declaration is the parsed form of `@macro name(params) { body }`.
type Declaration struct {
	Name   string
	Params []string
	Body   string
}

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive},
		OnPreprocess: func(meta map[string]any) core.PreprocessContributions {
			declarations, _ := meta[DeclarationKey].([]Declaration)
			if len(declarations) == 0 {
				return core.PreprocessContributions{}
			}
			byName := make(map[string]Declaration, len(declarations))
			for _, declaration := range declarations {
				byName[declaration.Name] = declaration
			}
			return core.PreprocessContributions{
				Primaries: []core.PrimaryExtension{&macroPrimary{byName: byName}},
			}
		},
		SelfTest: selftest,
	}
}
