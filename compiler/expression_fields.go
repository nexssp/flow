package compiler

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"time"
)

const maxExpressionProjectionDepth = 256

// ExpressionProjector exposes a typed struct as an expression map using the
// same JSON field names the compiler uses for static analysis. Scalar values
// keep their declared Go types; omitempty and custom JSON codecs do not change
// this in-memory expression view.
type ExpressionProjector struct {
	payloadType reflect.Type
	plan        *expressionProjectionPlan
}

type expressionProjectionPlan struct {
	kind        reflect.Kind
	projectable bool
	fields      []expressionProjectionField
	elem        *expressionProjectionPlan
}

type expressionProjectionField struct {
	name  string
	index []int
	plan  *expressionProjectionPlan
}

// NewExpressionProjector validates a struct payload type and compiles a
// reusable projection plan for expression evaluation.
func NewExpressionProjector(payloadType reflect.Type) (*ExpressionProjector, error) {
	for payloadType != nil && payloadType.Kind() == reflect.Pointer {
		payloadType = payloadType.Elem()
	}
	if payloadType == nil || payloadType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expression payload type must be a struct, got %v", payloadType)
	}
	plan, err := buildExpressionProjectionPlan(payloadType, make(map[reflect.Type]*expressionProjectionPlan))
	if err != nil {
		return nil, err
	}
	return &ExpressionProjector{payloadType: payloadType, plan: plan}, nil
}

// Project creates a JSON-tag-named expression map without JSON round-tripping
// the payload. Numbers, named scalar types, empty fields, and custom-marshaled
// structs therefore retain their declared in-memory values.
func (projector *ExpressionProjector) Project(payload any) (map[string]any, error) {
	if projector == nil || projector.plan == nil {
		return nil, errors.New("expression projector is nil")
	}
	actualType := reflect.TypeOf(payload)
	for actualType != nil && actualType.Kind() == reflect.Pointer {
		actualType = actualType.Elem()
	}
	if actualType == nil || actualType != projector.payloadType {
		return nil, fmt.Errorf("payload type %T does not match event contract %s", payload, projector.payloadType)
	}
	value := reflect.ValueOf(payload)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, errors.New("event payload must not be nil")
		}
		value = value.Elem()
	}
	projected, err := projectExpressionValue(value, projector.plan, 0)
	if err != nil {
		return nil, err
	}
	binding, ok := projected.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("event payload projection returned %T, want map[string]any", projected)
	}
	return binding, nil
}

func buildExpressionProjectionPlan(valueType reflect.Type, plans map[reflect.Type]*expressionProjectionPlan) (*expressionProjectionPlan, error) {
	if existing, ok := plans[valueType]; ok {
		return existing, nil
	}
	plan := &expressionProjectionPlan{kind: valueType.Kind()}
	plans[valueType] = plan

	//nolint:exhaustive // Unsupported kinds are rejected by the default case.
	switch valueType.Kind() {
	case reflect.Map:
		keyType := valueType.Key()
		if keyType.Kind() != reflect.String || keyType.Implements(reflect.TypeFor[encoding.TextMarshaler]()) ||
			keyType.Kind() != reflect.Pointer && reflect.PointerTo(keyType).Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
			return nil, fmt.Errorf("map key type %s cannot be projected as an expression object", keyType)
		}
		elem, err := buildExpressionProjectionPlan(valueType.Elem(), plans)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
		plan.projectable = elem.projectable
	case reflect.Pointer, reflect.Array, reflect.Slice:
		elem, err := buildExpressionProjectionPlan(valueType.Elem(), plans)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
		plan.projectable = elem.projectable
	case reflect.Struct:
		fields, err := jsonTypeFields(valueType)
		if err != nil {
			return nil, err
		}
		plan.projectable = len(fields) > 0
		if !plan.projectable {
			if fieldPath, containsReference := expressionMutableReferencePath(valueType, valueType.String(), make(map[reflect.Type]bool)); containsReference {
				return nil, fmt.Errorf("opaque expression leaf type %s contains mutable reference field %q and cannot be safely detached", valueType, fieldPath)
			}
		}
		for _, field := range fields {
			if !expressionFieldPathExported(valueType, field.index) {
				return nil, fmt.Errorf("expression field %q is reached through an unexported embedded field", field.name)
			}
			fieldPlan, err := buildExpressionProjectionPlan(field.typeOf, plans)
			if err != nil {
				return nil, fmt.Errorf("expression field %q: %w", field.name, err)
			}
			plan.fields = append(plan.fields, expressionProjectionField{
				name:  field.name,
				index: append([]int(nil), field.index...),
				plan:  fieldPlan,
			})
		}
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.String:
		// Statically analyzable scalar leaf.
	case reflect.Interface:
		return nil, fmt.Errorf("interface type %s is not a statically typed expression payload field", valueType)
	default:
		return nil, fmt.Errorf("type %s is not supported in an expression payload", valueType)
	}
	return plan, nil
}

func expressionMutableReferencePath(valueType reflect.Type, path string, seen map[reflect.Type]bool) (string, bool) {
	if valueType == reflect.TypeFor[time.Time]() {
		return "", false
	}
	//nolint:exhaustive // Other kinds are scalar values with no reference graph to detach.
	switch valueType.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return path, true
	case reflect.Array:
		return expressionMutableReferencePath(valueType.Elem(), path+"[]", seen)
	case reflect.Struct:
		if seen[valueType] {
			return "", false
		}
		seen[valueType] = true
		for field := range valueType.Fields() {
			if fieldPath, found := expressionMutableReferencePath(field.Type, path+"."+field.Name, seen); found {
				return fieldPath, true
			}
		}
	}
	return "", false
}

func expressionFieldPathExported(valueType reflect.Type, index []int) bool {
	for _, fieldIndex := range index {
		for valueType.Kind() == reflect.Pointer {
			valueType = valueType.Elem()
		}
		if valueType.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= valueType.NumField() {
			return false
		}
		field := valueType.Field(fieldIndex)
		if !field.IsExported() {
			return false
		}
		valueType = field.Type
	}
	return true
}

//nolint:nilnil // A nil projected value is the expression view's explicit JSON-like null.
func projectExpressionValue(value reflect.Value, plan *expressionProjectionPlan, depth int) (any, error) {
	if depth > maxExpressionProjectionDepth {
		return nil, fmt.Errorf("expression payload exceeds maximum projection depth %d", maxExpressionProjectionDepth)
	}
	if !value.IsValid() {
		return nil, nil
	}
	if (plan.kind == reflect.Pointer || plan.kind == reflect.Map || plan.kind == reflect.Slice) && value.IsNil() {
		return nil, nil
	}
	if plan.kind == reflect.Struct && !plan.projectable {
		return projectExpressionLeaf(value)
	}
	// Maps, slices, and arrays must always be traversed—even when their leaves
	// are scalar—so the expression binding never shares mutable containers.
	//nolint:exhaustive // Only reference-bearing/container kinds need special traversal; scalar leaves fall through.
	switch plan.kind {
	case reflect.Pointer, reflect.Struct, reflect.Array, reflect.Slice, reflect.Map:
		return projectExpressionContainer(value, plan, depth)
	default:
		return projectExpressionLeaf(value)
	}
}

//nolint:nilnil // A nil projected value is the expression view's explicit JSON-like null.
func projectExpressionLeaf(value reflect.Value) (any, error) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	if !value.CanInterface() {
		return nil, fmt.Errorf("expression payload field of type %s is not accessible", value.Type())
	}
	return value.Interface(), nil
}

func projectExpressionContainer(value reflect.Value, plan *expressionProjectionPlan, depth int) (any, error) {
	//nolint:exhaustive // Only projected container kinds are traversed; opaque leaves return above.
	switch plan.kind {
	case reflect.Pointer:
		return projectExpressionValue(value.Elem(), plan.elem, depth+1)
	case reflect.Struct:
		return projectExpressionStruct(value, plan, depth+1)
	case reflect.Array, reflect.Slice:
		return projectExpressionSequence(value, plan, depth)
	case reflect.Map:
		return projectExpressionMap(value, plan, depth)
	default:
		return nil, fmt.Errorf("unsupported expression projection kind %s", plan.kind)
	}
}

func projectExpressionSequence(value reflect.Value, plan *expressionProjectionPlan, depth int) ([]any, error) {
	items := make([]any, value.Len())
	for index := range value.Len() {
		item, err := projectExpressionValue(value.Index(index), plan.elem, depth+1)
		if err != nil {
			return nil, fmt.Errorf("expression payload item %d: %w", index, err)
		}
		items[index] = item
	}
	return items, nil
}

func projectExpressionMap(value reflect.Value, plan *expressionProjectionPlan, depth int) (map[string]any, error) {
	projected := make(map[string]any, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		item, err := projectExpressionValue(iterator.Value(), plan.elem, depth+1)
		if err != nil {
			return nil, fmt.Errorf("expression payload map value: %w", err)
		}
		projected[iterator.Key().String()] = item
	}
	return projected, nil
}

func projectExpressionStruct(value reflect.Value, plan *expressionProjectionPlan, depth int) (map[string]any, error) {
	binding := make(map[string]any, len(plan.fields))
	for _, field := range plan.fields {
		fieldValue, ok := expressionFieldValue(value, field.index)
		if !ok {
			binding[field.name] = nil
			continue
		}
		projected, err := projectExpressionValue(fieldValue, field.plan, depth+1)
		if err != nil {
			return nil, fmt.Errorf("expression payload field %q: %w", field.name, err)
		}
		binding[field.name] = projected
	}
	return binding, nil
}

func expressionFieldValue(value reflect.Value, index []int) (reflect.Value, bool) {
	for _, fieldIndex := range index {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return reflect.Value{}, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= value.NumField() {
			return reflect.Value{}, false
		}
		value = value.Field(fieldIndex)
	}
	return value, true
}
