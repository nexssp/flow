package compiler

import (
	"fmt"
	"math"
	"reflect"

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
	// Optional suppresses the default required-field check for capability
	// references. It has no effect on ArgAny, ArgString, ArgInt, or ArgBool.
	Optional bool
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
	for _, field := range schema {
		value, present := atom.Args[field.Name]
		if !present {
			if err := validateMissingArgField(atom, field); err != nil {
				return err
			}
			continue
		}

		if err := resolvePresentArgField(resolver, atom, field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateMissingArgField(atom *Atom, field ArgFieldSpec) error {
	// The compiler cannot know the keys in a runtime spread. Defer required-field
	// validation to the capability receiving it.
	if len(atom.ArgSpreads) > 0 || !isCapabilityRefKind(field.Kind) || field.Optional {
		return nil
	}
	return missingCapabilityFieldError(atom, field)
}

func resolvePresentArgField(resolver CapabilityResolver, atom *Atom, field ArgFieldSpec, value *Value) error {
	switch field.Kind {
	case ArgAny:
		// Untyped fields have no compile-time validation.
		return nil
	case ArgString, ArgInt, ArgBool:
		if !primitiveArgValueMatches(field.Kind, value) {
			got := "missing value"
			if value != nil {
				got = kindName(value.Kind)
			}
			if field.Kind == ArgInt && value != nil && value.Kind == ValueNumber && !isIntegerNumber(value.Num) {
				got = "non-integer number"
			}
			return SourceError(atom.Pos, "%s.%s: expected %s, got %s", atom.Name, field.Name, expectedArgKind(field.Kind), got)
		}
		return nil
	case ArgCapabilityRef:
		canonical, err := resolveCapabilityValue(resolver, value, atom.Name, field.Name)
		if err != nil {
			return err
		}
		value.Kind = ValueString
		value.Str = canonical
	case ArgCapabilityRefList:
		if value == nil || value.Kind != ValueSlice {
			got := "missing value"
			if value != nil {
				got = kindName(value.Kind)
			}
			return xerr.Validation(fmt.Sprintf(
				"%s.%s: expected a capability reference list, got %s",
				atom.Name, field.Name, got))
		}
		for _, item := range value.Slice {
			canonical, err := resolveCapabilityValue(resolver, item, atom.Name, field.Name)
			if err != nil {
				return err
			}
			item.Kind = ValueString
			item.Str = canonical
		}
		return nil
	}
	return nil
}

func primitiveArgValueMatches(kind ArgKind, value *Value) bool {
	if value == nil {
		return false
	}
	// These values depend on pipeline input or runtime expression evaluation;
	// their concrete types cannot be proven during compilation.
	if value.Kind == ValueRef || value.Kind == ValueExpression {
		return true
	}
	if value.Kind == ValueTyped {
		reflected := reflect.ValueOf(value.Typed)
		if !reflected.IsValid() {
			return false
		}
		switch kind {
		case ArgString:
			return reflected.Kind() == reflect.String
		case ArgBool:
			return reflected.Kind() == reflect.Bool
		case ArgInt:
			switch reflected.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return true
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return reflected.Uint() <= uint64(1<<63-1)
			case reflect.Float32, reflect.Float64:
				return isIntegerNumber(reflected.Float())
			case reflect.Invalid, reflect.Bool, reflect.Uintptr, reflect.Complex64, reflect.Complex128,
				reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
				reflect.Pointer, reflect.Slice, reflect.String, reflect.Struct, reflect.UnsafePointer:
				return false
			}
		case ArgAny, ArgCapabilityRef, ArgCapabilityRefList:
			return false
		}
		return false
	}

	switch kind {
	case ArgString:
		return value.Kind == ValueString || value.Kind == ValueBare
	case ArgInt:
		return value.Kind == ValueNumber && isIntegerNumber(value.Num)
	case ArgBool:
		return value.Kind == ValueBool
	case ArgAny, ArgCapabilityRef, ArgCapabilityRefList:
		return false
	}
	return false
}

func isIntegerNumber(value float64) bool {
	const int64Limit = float64(1 << 63)
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= -int64Limit && value < int64Limit && math.Trunc(value) == value
}

func expectedArgKind(kind ArgKind) string {
	switch kind {
	case ArgString:
		return stringKindName
	case ArgInt:
		return "integer"
	case ArgBool:
		return "bool"
	case ArgAny, ArgCapabilityRef, ArgCapabilityRefList:
		return "value"
	}
	return "value"
}

func isCapabilityRefKind(kind ArgKind) bool {
	return kind == ArgCapabilityRef || kind == ArgCapabilityRefList
}

func missingCapabilityFieldError(atom *Atom, field ArgFieldSpec) error {
	example := "runtime.noop"
	if field.Kind == ArgCapabilityRefList {
		example = "[runtime.fail, runtime.const]"
	}
	return SourceError(atom.Pos,
		`%s: field %q is a required capability reference and must be declared in the atom's @{ ... } block, not supplied only through pipeline input`+
			"\n  hint: %s @{ %s: %s }",
		atom.Name, field.Name, atom.Name, field.Name, example)
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
	case ValueNumber, ValueBool, ValueNull, ValueMap, ValueSlice, ValueTyped, ValueExpression:
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
		return stringKindName
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
	case ValueTyped:
		return "typed value"
	case ValueExpression:
		return "expression"
	default:
		return "unknown"
	}
}
