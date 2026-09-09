package nodes

import (
	"fmt"
	"reflect"

	"github.com/nexssp/transport/codec"
)

func decodePayload(input any, target any) error {
	if target == nil {
		return nil
	}

	// ⚡ 1. Direct any pointer assignment (0 allocs)
	if ptr, ok := target.(*any); ok {
		*ptr = input
		return nil
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("flow: decode target must be a non-nil pointer")
	}

	targetElem := rv.Elem()

	// ⚡ 2. Fast type assignment without serialization (0 allocs)
	if input != nil {
		iv := reflect.ValueOf(input)
		if iv.Type().AssignableTo(targetElem.Type()) {
			targetElem.Set(iv)
			return nil
		}
	}

	// 3. Fallback to serialization only across distinct struct schemas
	data, err := codec.Default.Marshal(input)
	if err != nil {
		return fmt.Errorf("flow: marshal input: %w", err)
	}
	if err := codec.Default.Unmarshal(data, target); err != nil {
		return fmt.Errorf("flow: unmarshal target: %w", err)
	}

	return nil
}
