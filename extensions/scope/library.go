package scope

import (
	"embed"
	"strconv"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "scope"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []compiler.Directive{Directive, ProfileDirective},
		OnPreprocess: func(meta map[string]any) compiler.PreprocessContributions {
			spans := spansFromMeta(meta)
			if len(spans) == 0 {
				return compiler.PreprocessContributions{}
			}
			return compiler.PreprocessContributions{
				CompileOpts: []compiler.CompileOption{compiler.WithLineModifiers(makeLookups(spans)...)},
			}
		},
		Fixtures: fixturesFS,
	}
}

// makeLookups returns one lookup per span. Spans are visited in append
// order (outer-to-inner), so later lookups win on modifier-name
// collision inside prependInherited. Each lookup carries its own
// source so `nflow explain` can attribute a modifier to the specific
// @scope block that produced it.
func makeLookups(spans []span) []compiler.LineLookup {
	out := make([]compiler.LineLookup, 0, len(spans))
	for _, s := range spans {
		out = append(out, compiler.LineLookup{
			Source: compiler.ModifierSource{
				Kind:  "scope",
				Label: "line " + strconv.Itoa(s.start),
			},
			Fn: func(line int) []string {
				if s.start <= line && line <= s.end {
					return s.modifiers
				}
				return nil
			},
		})
	}
	return out
}
