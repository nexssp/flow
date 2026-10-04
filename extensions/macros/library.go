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
			// Always install the macro primary, even when this source declares
			// no macros. This is what makes `@unknown_name` produce
			// "unknown macro @unknown_name" instead of the parser's generic
			// "unexpected token".
			//
			// Sub-pipelines inherit the parent's macro primary through
			// CompileReq.InheritedPrimaries and also contribute their own here.
			// NewPrimaryExtensionTable detects the duplicate TokAtPrompt handler
			// and, because *macroPrimary implements core.MergeablePrimary,
			// calls MergeWith to fold the two declaration maps together. The
			// parent's macros stay visible in the sub-source; the sub-source's
			// own declarations shadow on name collision.
			declarations, _ := meta[DeclarationKey].([]Declaration)
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
