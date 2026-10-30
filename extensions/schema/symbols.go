package schema

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
)

// resolveSchemaSymbols replaces legacy string references in schema tags and
// preserves explicit typed symbol values for type-directed default/validation
// handling. Local/imported symbols shadow globals as they do elsewhere.
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
			resolved, err := resolveDefaultSymbol(field, raw, local, global)
			if err != nil {
				return fmt.Errorf("field %q default: %w", field.Name, err)
			}
			field.Tags["default"] = resolved
		}
		if raw, ok := field.Tags["validate"]; ok {
			resolved, err := resolveValidationSymbols(field, raw, local, global)
			if err != nil {
				return fmt.Errorf("field %q validate: %w", field.Name, err)
			}
			field.Tags["validate"] = resolved
		}
	}
	return nil
}

func resolveDefaultSymbol(field *Field, raw string, local, global map[string]compiler.SymbolNamespace) (string, error) {
	value, found, known := lookupSchemaSymbol(raw, local, global)
	if !known {
		return raw, nil
	}
	if !found {
		return "", fieldSourceError(field, "unknown symbol %q", raw)
	}
	if typed, ok := value.(compiler.TypedSymbolValue); ok {
		if !typedDefaultMatches(field, typed) {
			return "", fieldSourceError(field,
				"symbol %q has explicit type %s, incompatible with default field type %q", raw, typed.Type, field.Type)
		}
		field.TypedDefault = typed.Value
		field.HasTypedDefault = true
		return raw, nil
	}
	resolved, ok := value.(string)
	if !ok {
		return "", fieldSourceError(field,
			"symbol %q has type %T; schema tags require string symbols or a matching typed symbol", raw, value)
	}
	return resolved, nil
}

func typedDefaultMatches(field *Field, value compiler.TypedSymbolValue) bool {
	if field.Kind == KindAny {
		return true
	}
	switch value.Type {
	case "string":
		return field.Kind == KindString
	case "int":
		return field.Kind == KindInt
	case "float64":
		return field.Kind == KindFloat
	case "bool":
		return field.Kind == KindBool
	case "duration":
		return field.Kind == KindDuration
	default:
		return false
	}
}

func resolveValidationSymbols(field *Field, raw string, local, global map[string]compiler.SymbolNamespace) (string, error) {
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
				return "", fieldSourceError(field, "unknown symbol %q", word)
			}
			if typed, ok := value.(compiler.TypedSymbolValue); ok {
				resolved, err := typedValidationOperand(field, name, word, typed)
				if err != nil {
					return "", err
				}
				words[j] = resolved
				continue
			}
			resolved, ok := value.(string)
			if !ok {
				return "", fieldSourceError(field,
					"symbol %q has type %T; validation tags require string symbols or a matching typed symbol", word, value)
			}
			words[j] = resolved
		}
		rules[i] = name + "=" + strings.Join(words, " ")
	}
	return strings.Join(rules, ","), nil
}

func typedValidationOperand(field *Field, ruleName, reference string, symbol compiler.TypedSymbolValue) (string, error) {
	switch symbol.Type {
	case "int":
		return typedIntValidationOperand(field, ruleName, reference, symbol.Value)
	case "float64":
		return typedFloatValidationOperand(field, ruleName, reference, symbol.Value)
	case "duration":
		return typedDurationValidationOperand(field, ruleName, reference, symbol.Value)
	case "string":
		return typedStringValidationOperand(field, ruleName, reference, symbol.Value)
	case "bool":
		return typedBoolValidationOperand(field, ruleName, reference, symbol.Value)
	default:
		return "", typedValidationMismatch(field, ruleName, reference, symbol.Type)
	}
}

func typedIntValidationOperand(field *Field, ruleName, reference string, raw any) (string, error) {
	value, ok := raw.(int)
	if field.Kind != KindInt || !numericValidationRule(ruleName) || !ok {
		return "", typedValidationMismatch(field, ruleName, reference, "int")
	}
	return strconv.Itoa(value), nil
}

func typedFloatValidationOperand(field *Field, ruleName, reference string, raw any) (string, error) {
	value, ok := raw.(float64)
	if field.Kind != KindFloat || !numericValidationRule(ruleName) || !ok {
		return "", typedValidationMismatch(field, ruleName, reference, "float64")
	}
	return strconv.FormatFloat(value, 'g', -1, 64), nil
}

func typedDurationValidationOperand(field *Field, ruleName, reference string, raw any) (string, error) {
	value, ok := raw.(time.Duration)
	if field.Kind != KindDuration || !durationComparisonRule(ruleName) || !ok {
		return "", typedValidationMismatch(field, ruleName, reference, "duration")
	}
	// Validator numeric comparisons on time.Duration use its native int64
	// representation. The bridge is explicitly nanoseconds, not a Go duration
	// string that the validator cannot parse.
	return strconv.FormatInt(int64(value), 10), nil
}

func typedStringValidationOperand(field *Field, ruleName, reference string, raw any) (string, error) {
	value, ok := raw.(string)
	if field.Kind != KindString || !stringValidationRule(ruleName) || !ok {
		return "", typedValidationMismatch(field, ruleName, reference, "string")
	}
	if strings.ContainsAny(value, ", \t\r\n") || value == "" {
		return "", fieldSourceError(field,
			"typed string symbol %q is not a safe operand for validation rule %q", reference, ruleName)
	}
	return value, nil
}

func typedBoolValidationOperand(field *Field, ruleName, reference string, raw any) (string, error) {
	value, ok := raw.(bool)
	if field.Kind != KindBool || !stringValidationRule(ruleName) || !ok {
		return "", typedValidationMismatch(field, ruleName, reference, "bool")
	}
	return strconv.FormatBool(value), nil
}

func typedValidationMismatch(field *Field, ruleName, reference, typeName string) error {
	return fieldSourceError(field,
		"symbol %q has explicit type %s, incompatible with validation rule %q on field type %q",
		reference, typeName, ruleName, field.Type)
}

func numericValidationRule(name string) bool {
	switch name {
	case "eq", "ne", "gt", "gte", "lt", "lte", "min", "max", "oneof":
		return true
	default:
		return false
	}
}

func durationComparisonRule(name string) bool {
	switch name {
	case "eq", "ne", "gt", "gte", "lt", "lte", "min", "max":
		return true
	default:
		return false
	}
}

func stringValidationRule(name string) bool {
	switch name {
	case "eq", "ne", "oneof":
		return true
	default:
		return false
	}
}

func fieldSourceError(field *Field, format string, args ...any) error {
	if field != nil && field.Position.Line > 0 {
		return compiler.SourceError(field.Position, format, args...)
	}
	return fmt.Errorf(format, args...)
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
