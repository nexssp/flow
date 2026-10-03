// Package macros extends the compiler with a @macro engine.
//
// A @macro directive stores a declaration; OnPreprocess turns each
// declaration into a PrimaryExtension dispatching on TokAtPrompt; and
// every expansion re-enters the parser with the caller's arguments
// substituted into the body.
//
// Typical use:
//
//	@macro greet(name) { runtime.const @{ value: $name } }
//	@greet("World")   // expands to: runtime.const @{ value: "World" }
//
// Without this bundle, @name is "unexpected token @name".
package macros

import (
	"embed"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "macros"

// DeclarationKey is the meta slot where @macro declarations land before
// OnPreprocess converts them to a PrimaryExtension.
const DeclarationKey = "macros"

//go:embed nflows
var fixturesFS embed.FS

// Declaration is the parsed form of `@macro name(params) { body }`.
//
// DefLine and BodyLine are 1-based file lines. DefLine is the line of
// the @macro directive itself. BodyLine is the line where body content
// begins — the same line for an inline body like `@macro x { ... }`,
// or the line after the opening brace for a multi-line body. They are
// used to remap parse errors from inside the expanded body back to
// the correct file position so diagnostics are clickable.
type Declaration struct {
	Name     string
	Params   []string
	Body     string
	DefLine  int
	BodyLine int
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
			// Only install the primary when this source declares at
			// least one macro. Installing it unconditionally collides
			// with the inherited top-level primary inside sub-pipeline
			// compiles: NewPrimaryExtensionTable panics on a duplicate
			// TokAtPrompt handler. Sub-sources that declare nothing
			// inherit the top primary through InheritedPrimaries and
			// see the top-level declarations through it.
			//
			// Consequence: a file that uses @name without declaring any
			// macro gets the parser's generic "unexpected token" error
			// rather than "unknown macro". That is a rare shape — a
			// macro-aware file almost always declares at least one.
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
		Fixtures: fixturesFS,
	}
}
