package core

import (
	"fmt"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// ArgKind describes the compiler-level type of one capability argument.
type ArgKind uint8

const (
	// ArgAny is untyped; no compile-time validation applies.
	ArgAny ArgKind = iota
	// ArgString is a plain string; the value is passed through.
	ArgString
	// ArgInt is an integer literal.
	ArgInt
	// ArgBool is a boolean literal.
	ArgBool
	// ArgCapabilityRef is a statically resolved reference to an action.
	// The parser must present a bare identifier; quoted strings are
	// rejected, and the value is translated through TranslateKeyword
	// and verified against the resolver at compile time.
	ArgCapabilityRef
	// ArgCapabilityRefList is a list of ArgCapabilityRef.
	ArgCapabilityRefList
)

// ArgFieldSpec declares the compiler-level kind of one argument of a
// capability. Bundles publish their specs via Bundle.ArgSchemas.
type ArgFieldSpec struct {
	Name string
	Kind ArgKind
}

// argSchemaAdvisor returns an AtomAdviseFunc that resolves and validates
// capability references inside the atom's arguments, using the schema
// declared by the owning bundle. It runs before buildPayload, so the
// handler receives canonical names.
func argSchemaAdvisor(resolver CapabilityResolver, schemas map[string][]ArgFieldSpec) AtomAdviseFunc {
	return func(atom *Atom, _ *action.Builder[any, any]) error {
		schema, ok := schemas[atom.Name]
		if !ok {
			return nil
		}
		return resolveArgSchema(resolver, atom, schema)
	}
}

func resolveArgSchema(resolver CapabilityResolver, atom *Atom, schema []ArgFieldSpec) error {
	kinds := make(map[string]ArgKind, len(schema))
	for _, f := range schema {
		kinds[f.Name] = f.Kind
	}

	for key, value := range atom.Args {
		kind, declared := kinds[key]
		if !declared || kind == ArgAny {
			continue
		}
		switch kind {
		case ArgAny:
			// Unreachable: the guard above skips ArgAny. Listed so the
			// exhaustive linter sees full coverage and a future guard
			// change cannot silently drop the case.
		case ArgCapabilityRef:
			canonical, err := resolveCapabilityValue(resolver, value, atom.Name, key)
			if err != nil {
				return err
			}
			value.Kind = ValueString
			value.Str = canonical
		case ArgCapabilityRefList:
			if value.Kind != ValueSlice {
				return xerr.Validation(fmt.Sprintf(
					"%s.%s: expected a capability reference list, got %s",
					atom.Name, key, kindName(value.Kind)))
			}
			for _, item := range value.Slice {
				canonical, err := resolveCapabilityValue(resolver, item, atom.Name, key)
				if err != nil {
					return err
				}
				item.Kind = ValueString
				item.Str = canonical
			}
		case ArgString, ArgInt, ArgBool:
			// Value-kind validation lands in Phase 2b.
		}
	}
	return nil
}

func resolveCapabilityValue(resolver CapabilityResolver, v *Value, atomName, fieldName string) (string, error) {
	if v == nil {
		return "", xerr.Validation(fmt.Sprintf("%s.%s: missing capability reference", atomName, fieldName))
	}
	switch v.Kind {
	case ValueBare:
		// ok
	case ValueString:
		return "", xerr.Validation(fmt.Sprintf(
			"%s.%s: use a bare capability reference, not a string literal (%q)",
			atomName, fieldName, v.Str))
	case ValueRef:
		return "", xerr.Validation(fmt.Sprintf(
			"%s.%s: dynamic capability references are not supported; use a static identifier",
			atomName, fieldName))
	case ValueNumber, ValueBool, ValueNull, ValueMap, ValueSlice:
		return "", xerr.Validation(fmt.Sprintf(
			"%s.%s: expected capability reference, got %s",
			atomName, fieldName, kindName(v.Kind)))
	}

	name := v.Str
	if canonical, ok := TranslateKeyword(name); ok {
		name = canonical
	}
	if _, ok := resolver.Action(name); ok {
		return name, nil
	}
	if _, ok := resolver.Stream(name); ok {
		return "", xerr.Validation(fmt.Sprintf(
			"%s.%s: %q is a stream, expected an action", atomName, fieldName, name))
	}
	if _, ok := resolver.Operator(name); ok {
		return "", xerr.Validation(fmt.Sprintf(
			"%s.%s: %q is an operator, expected an action", atomName, fieldName, name))
	}
	return "", xerr.Validation(fmt.Sprintf(
		"%s.%s: unknown capability %q", atomName, fieldName, name))
}

func kindName(k ValueKind) string {
	switch k {
	case ValueString:
		return "string"
	case ValueBare:
		return "bare identifier"
	case ValueNumber:
		return "number"
	case ValueBool:
		return "bool"
	case ValueNull:
		return "null"
	case ValueRef:
		return "state reference"
	case ValueMap:
		return "object"
	case ValueSlice:
		return "array"
	default:
		return "unknown"
	}
}
