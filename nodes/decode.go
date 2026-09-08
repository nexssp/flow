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

	if ptr, ok := target.(*any); ok {
		*ptr = input
		return nil
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("flow: decode target must be a non-nil pointer")
	}

	iv := reflect.ValueOf(input)
	if iv.IsValid() && iv.Type().AssignableTo(rv.Type().Elem()) {
		rv.Elem().Set(iv)
		return nil
	}

	data, err := codec.Default.Marshal(input)
	if err != nil {
		return fmt.Errorf("flow: marshal loop input: %w", err)
	}
	if err := codec.Default.Unmarshal(data, target); err != nil {
		return fmt.Errorf("flow: unmarshal loop target: %w", err)
	}

	return nil
}
