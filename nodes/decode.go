package nodes

import (
	"fmt"
	"reflect"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/codec"
)

// decodePayload decodes an input into target using fast-path type assignment,
// kernel coercion, and only falls back to serialization across mismatched schemas.
func decodePayload(input any, target any) error {
	if target == nil {
		return nil
	}

	// 1. Direct pointer to any (*any) fast-path (0 allocs)
	if ptr, ok := target.(*any); ok {
		*ptr = input

		return nil
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("flow: decode target must be a non-nil pointer, got %T", target)
	}

	targetElem := rv.Elem()

	// 2. Direct assignability check (0 allocs)
	if input != nil {
		iv := reflect.ValueOf(input)
		if iv.Type().AssignableTo(targetElem.Type()) {
			targetElem.Set(iv)

			return nil
		}
		// If input is a pointer to the element type (*T -> T)
		if iv.Kind() == reflect.Pointer && !iv.IsNil() && iv.Elem().Type().AssignableTo(targetElem.Type()) {
			targetElem.Set(iv.Elem())

			return nil
		}
	}

	// 3. Fallback: Fast JSON/Codec bridge
	data, err := codec.Default.Marshal(input)
	if err != nil {
		return fmt.Errorf("flow: marshal input (%T): %w", input, err)
	}

	if err := codec.Default.Unmarshal(data, target); err != nil {
		return fmt.Errorf("flow: unmarshal target (%T): %w", target, err)
	}

	return nil
}

var _ = action.Coerce[any]
