package schema

import (
	"slices"
	"strings"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

// schemaRefAdvisor rejects atoms whose schema modifier names neither a local
// schema nor a schema explicitly imported under a module alias. Called per atom
// during Build, before modifiers are applied to the builder, so a typo fails
// at compile time with a source position instead of surfacing at runtime.
//
// Empty declared set means the enclosing source declared no schemas at
// all — the advisor no-ops so that a @pipeline body inheriting its
// parent's advisor (see runner.compileSub) does not reject references
// against its own empty local set.
func schemaRefAdvisor(meta map[string]any) compiler.AtomAdviseFunc {
	declared := make(map[string]Schema)
	for _, s := range VisibleSchemasFromMeta(meta) {
		declared[s.Name] = s
	}
	if len(declared) == 0 {
		return nil
	}
	known := declaredNames(declared)
	return func(atom *compiler.Atom, _ *action.Builder[any, any]) error {
		for _, raw := range atom.Modifiers {
			modifierName := compiler.ModifierName(raw)
			if modifierName != SchemaModifierName && modifierName != RequestSchemaModifierName && modifierName != ResponseSchemaModifierName && modifierName != InSchemaModifierName && modifierName != OutSchemaModifierName {
				continue
			}
			_, name, _ := strings.Cut(raw, "=")
			if name == "" {
				return compiler.SourceError(atom.Pos,
					":%s requires a name (use :%s=Name)", modifierName, modifierName)
			}
			if _, exists := declared[name]; !exists {
				return compiler.SourceError(atom.Pos,
					":%s=%s: unknown schema (declared: %s)", modifierName, name, known)
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

// wireSchemaAdvisor is the OnPreprocess hook: it reads the current source's
// local and explicitly imported schemas and installs an advisor that validates
// schema references in that source and its nested pipeline bodies.
func wireSchemaAdvisor(meta map[string]any) compiler.PreprocessContributions {
	advisor := schemaRefAdvisor(meta)
	if advisor == nil {
		return compiler.PreprocessContributions{}
	}
	return compiler.PreprocessContributions{
		CompileOpts: []compiler.CompileOption{compiler.WithAtomAdvisors(advisor)},
	}
}
