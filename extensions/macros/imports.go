package macros

import (
	"maps"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// importedMacroDeclarations exposes a module's macros under its include alias.
// Nested source imports keep their own qualified path beneath that alias.
func importedMacroDeclarations(meta map[string]any, namespace string) map[string]Declaration {
	declarations, _ := collectImportedMacros(meta, namespace)
	return declarations
}

func collectImportedMacros(meta map[string]any, namespace string) (macros map[string]Declaration, bindings map[string]string) {
	declarations, _ := meta[DeclarationKey].([]Declaration)
	macros = make(map[string]Declaration, len(declarations))
	bindings = make(map[string]string, len(declarations))
	for _, declaration := range declarations {
		bindings[declaration.Name] = namespace + "." + declaration.Name
	}

	for _, imported := range compiler.SourceImports(meta) {
		if imported.Source == nil {
			continue
		}
		nestedNamespace := namespace + "." + imported.Namespace
		nestedMacros, nestedBindings := collectImportedMacros(imported.Source.Metadata(), nestedNamespace)
		// Include aliases are unique within a source, so these qualified paths cannot overlap.
		maps.Copy(macros, nestedMacros)
		for name, qualified := range nestedBindings {
			bindings[imported.Namespace+"."+name] = qualified
		}
	}

	for _, declaration := range declarations {
		declaration.Body = qualifyMacroReferences(declaration.Body, bindings)
		macros[namespace+"."+declaration.Name] = declaration
	}
	return macros, bindings
}

// qualifyMacroReferences makes references inside an imported macro point at
// the same module tree even though the macro body is expanded by its caller.
func qualifyMacroReferences(body string, bindings map[string]string) string {
	var out strings.Builder
	out.Grow(len(body))
	for i := 0; i < len(body); {
		switch body[i] {
		case '"', '\'', '`':
			next := skipQuoted(body, i)
			out.WriteString(body[i:next])
			i = next
		case '#':
			next := skipLineComment(body, i)
			out.WriteString(body[i:next])
			i = next
		case '/':
			if i+1 < len(body) && body[i+1] == '/' {
				next := skipLineComment(body, i)
				out.WriteString(body[i:next])
				i = next
			} else {
				out.WriteByte(body[i])
				i++
			}
		default:
			if ref, next, ok := readQualifiedMacroRefAt(body, i); ok {
				if qualified, found := bindings[ref]; found {
					out.WriteByte('@')
					out.WriteString(qualified)
				} else {
					out.WriteString(body[i:next])
				}
				i = next
				continue
			}
			if body[i] == '@' {
				ref, next := readMacroRefAt(body, i)
				if qualified, found := bindings[ref]; ref != "" && found {
					out.WriteByte('@')
					out.WriteString(qualified)
					i = next
					continue
				}
			}
			out.WriteByte(body[i])
			i++
		}
	}
	return out.String()
}

// readQualifiedMacroRefAt reads `namespace.@name` and returns the equivalent
// binding key `namespace.name`. Namespace paths may contain nested aliases.
func readQualifiedMacroRefAt(source string, start int) (name string, next int, ok bool) {
	if start >= len(source) || !isMacroNameStart(source[start]) {
		return "", start, false
	}
	segmentStart := start
	for i := start; i < len(source); {
		if !isMacroNameStart(source[i]) {
			return "", start, false
		}
		i++
		for i < len(source) && (isMacroNameCont(source[i]) || source[i] == '-') {
			i++
		}
		if i >= len(source) || source[i] != '.' {
			return "", start, false
		}
		if i+1 < len(source) && source[i+1] == '@' {
			macro, end := readMacroRefAt(source, i+1)
			if macro == "" {
				return "", start, false
			}
			return source[segmentStart:i] + "." + macro, end, true
		}
		i++
	}
	return "", start, false
}
