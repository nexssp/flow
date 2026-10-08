package runtime

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/nexssp/kernel/action"
)

// Flatten unwraps a struct into a map so its fields become top-level
// keys in downstream @{} payloads.
//
// Why this exists: a pipe carries typed values. When an action returns
// a struct (distribute.map returns DistributeMapRes, a custom action
// returns its own DTO), the next action's @{} args are merged on top of
// a wrapper map, not the struct's fields. A ref like `items` or
// `.items` in the following @{} atom does not reach the struct field.
//
// Flatten solves it with one explicit step: marshal the struct to JSON,
// unmarshal it as a map, hand the map downstream. Now every field is a
// top-level key and @{} refs work as expected.
//
// map[string]any passes through untouched. Slices, primitives, and nil
// pass through untouched. Only structs (and pointers to structs) are
// converted, because they are the only shape whose fields cannot be
// addressed by downstream @{} refs.
var Flatten = action.New("runtime.flatten", func(_ context.Context, in any) (any, error) {
	if in == nil {
		return in, nil
	}
	if _, ok := in.(map[string]any); ok {
		return in, nil
	}
	rv := reflect.ValueOf(in)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return in, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return in, nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return in, nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return in, nil
	}
	return m, nil
}).
	Description("Unwrap a struct into a map so its fields flow as top-level keys").
	Tag("base", "shape").
	Build()
