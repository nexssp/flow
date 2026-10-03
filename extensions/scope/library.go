package scope

import (
	"embed"
	"strconv"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "scope"

//go:embed nflows
var fixturesFS embed.FS

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{Directive, ProfileDirective},
		OnPreprocess: func(meta map[string]any) core.PreprocessContributions {
			spans := spansFromMeta(meta)
			if len(spans) == 0 {
				return core.PreprocessContributions{}
			}
			return core.PreprocessContributions{
				CompileOpts: []core.CompileOption{core.WithLineModifiers(makeLookups(spans)...)},
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
func makeLookups(spans []span) []core.LineLookup {
	out := make([]core.LineLookup, 0, len(spans))
	for _, s := range spans {
		s := s
		out = append(out, core.LineLookup{
			Source: core.ModifierSource{
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
