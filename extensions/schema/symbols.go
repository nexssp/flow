package schema

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
)

// resolveSchemaSymbols replaces symbol references in schema defaults and
// validation-tag operands. Schemas may refer to symbols declared earlier in
// the same source, imported modules, or a registered global namespace.
func resolveSchemaSymbols(fields []Field, local map[string]compiler.SymbolNamespace) error {
	global := make(map[string]compiler.SymbolNamespace)
	for _, namespace := range compiler.Namespaces() {
		global[namespace.Name] = namespace
	}

	for i := range fields {
		field := &fields[i]
		if field.Tags == nil {
			continue
		}
		if raw, ok := field.Tags["default"]; ok {
			resolved, err := resolveDefaultSymbol(raw, local, global)
			if err != nil {
				return fmt.Errorf("field %q default: %w", field.Name, err)
			}
			field.Tags["default"] = resolved
		}
		if raw, ok := field.Tags["validate"]; ok {
			resolved, err := resolveValidationSymbols(raw, local, global)
			if err != nil {
				return fmt.Errorf("field %q validate: %w", field.Name, err)
			}
			field.Tags["validate"] = resolved
		}
	}
	return nil
}

func resolveDefaultSymbol(raw string, local, global map[string]compiler.SymbolNamespace) (string, error) {
	value, found, known := lookupSchemaSymbol(raw, local, global)
	if !known {
		return raw, nil
	}
	if !found {
		return "", fmt.Errorf("unknown symbol %q", raw)
	}
	resolved, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("symbol %q has type %T; schema tags require string symbols", raw, value)
	}
	return resolved, nil
}

func resolveValidationSymbols(raw string, local, global map[string]compiler.SymbolNamespace) (string, error) {
	rules := strings.Split(raw, ",")
	for i, rule := range rules {
		name, operand, hasOperand := strings.Cut(rule, "=")
		if !hasOperand {
			continue
		}
		words := strings.Fields(operand)
		for j, word := range words {
			value, found, known := lookupSchemaSymbol(word, local, global)
			if !known {
				continue
			}
			if !found {
				return "", fmt.Errorf("unknown symbol %q", word)
			}
			resolved, ok := value.(string)
			if !ok {
				return "", fmt.Errorf("symbol %q has type %T; schema tags require string symbols", word, value)
			}
			words[j] = resolved
		}
		rules[i] = name + "=" + strings.Join(words, " ")
	}
	return strings.Join(rules, ","), nil
}

func lookupSchemaSymbol(reference string, local, global map[string]compiler.SymbolNamespace) (value any, found, known bool) {
	namespaceName, memberPath, ok := strings.Cut(reference, ".")
	if !ok || namespaceName == "" || memberPath == "" {
		return nil, false, false
	}
	if namespace, exists := local[namespaceName]; exists {
		value, found = lookupSymbolPath(namespace.Symbols, memberPath)
		return value, found, true
	}
	if namespace, exists := global[namespaceName]; exists {
		value, found = lookupSymbolPath(namespace.Symbols, memberPath)
		return value, found, true
	}
	return nil, false, false
}

func lookupSymbolPath(root map[string]any, path string) (any, bool) {
	var current any = root
	for part := range strings.SplitSeq(path, ".") {
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = values[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
