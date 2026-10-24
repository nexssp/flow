package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/nexssp/kernel/xerr"
)

// NormalizeObject returns the JSON-visible fields of value as a generic object.
// It follows encoding/json semantics for field visibility, json tags, embedded
// fields, pointers, custom marshalers, and omitempty. Nested objects become
// map[string]any, arrays become []any, and JSON numbers become float64.
//
// A nil value, nil pointer, or value that encodes as a non-object is rejected.
// The input is not modified. Use the returned view for inspection or
// validation; retain the original value when typed output must be preserved.
func NormalizeObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, xerr.Validation(fmt.Sprintf("schema: encode object: %v", err))
	}
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, xerr.Validation(fmt.Sprintf("schema: expected object, got %T", value))
	}

	var normalized map[string]any
	if err := json.Unmarshal(trimmed, &normalized); err != nil {
		return nil, xerr.Validation(fmt.Sprintf("schema: decode object: %v", err))
	}
	return normalized, nil
}
