package scope

import (
	"embed"

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
				CompileOpts: []core.CompileOption{core.WithLineModifiers(makeLookup(spans))},
			}
		},
		Fixtures: fixturesFS,
	}
}

// makeLookup returns a line-indexed modifier function. Spans are
// visited in append order (outer-to-inner), so later modifiers override
// earlier ones on the same name. prependInherited handles the override;
// this function just concatenates.
func makeLookup(spans []span) func(int) []string {
	return func(line int) []string {
		var out []string
		for _, s := range spans {
			if s.start <= line && line <= s.end {
				out = append(out, s.modifiers...)
			}
		}
		return out
	}
}
