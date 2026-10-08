package schema

import (
	"slices"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// schemaRefAdvisor rejects atoms whose :schema= modifier names a schema
// that was not declared earlier in the same source. Called per atom
// during Build, before modifiers are applied to the builder, so a typo
// fails at compile time with a source position instead of surfacing as
// a missing-tag lookup at runtime.
//
// Empty declared set means the enclosing source declared no schemas at
// all — the advisor no-ops so that a @pipeline body inheriting its
// parent's advisor (see runner.compileSub) does not reject references
// against its own empty local set.
func schemaRefAdvisor(declared map[string]Schema) core.AtomAdviseFunc {
	if len(declared) == 0 {
		return nil
	}
	known := declaredNames(declared)
	return func(atom *core.Atom, _ *action.Builder[any, any]) error {
		for _, raw := range atom.Modifiers {
			if core.ModifierName(raw) != "schema" {
				continue
			}
			_, name, _ := strings.Cut(raw, "=")
			if name == "" {
				return core.SourceError(atom.Pos,
					":schema requires a name (use :schema=Name)")
			}
			if _, exists := declared[name]; !exists {
				return core.SourceError(atom.Pos,
					":schema=%s: unknown schema (declared: %s)", name, known)
			}
		}
		return nil
	}
}

func declaredNames(declared map[string]Schema) string {
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// wireSchemaAdvisor is the OnPreprocess hook: it reads the schemas the
// current source declared and installs an advisor that validates every
// :schema= reference in that source (and, via inherited compile options,
// in every nested pipeline body).
func wireSchemaAdvisor(meta map[string]any) core.PreprocessContributions {
	schemas := SchemasFromMap(meta)
	if len(schemas) == 0 {
		return core.PreprocessContributions{}
	}
	declared := make(map[string]Schema, len(schemas))
	for _, s := range schemas {
		declared[s.Name] = s
	}
	advisor := schemaRefAdvisor(declared)
	if advisor == nil {
		return core.PreprocessContributions{}
	}
	return core.PreprocessContributions{
		CompileOpts: []core.CompileOption{core.WithAtomAdvisors(advisor)},
	}
}
