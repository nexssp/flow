package runtime

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Const returns a fixed literal. Numbers, bools, JSON objects, JSON
// arrays, and JSON-quoted strings are auto-coerced from the string
// representation so the DSL stays readable: const @{ value: 42 }
// produces int64(42), not "42".
var Const = action.New("const", func(_ context.Context, in any) (any, error) {
	if s, ok := in.(string); ok {
		return coerceLiteral(s), nil
	}

	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("const: expected object with 'value' key or raw scalar, got " + typeName(in))
	}

	raw, ok := m["value"]
	if !ok {
		raw, ok = m["val"]
	}
	if !ok {
		return nil, xerr.BadRequest("const: missing required field 'value'")
	}
	return coerceLiteral(raw), nil
}).Description("Return a fixed literal; numbers, bools, and JSON auto-parse").
	Tag("base", "literal").
	Build()

// coerceLiteral turns a string into its natural type when the string
// looks like JSON, a number, or a bool. Everything else is returned
// as-is.
func coerceLiteral(raw any) any {
	if number, ok := raw.(float64); ok {
		const twoTo63 = 1 << 63
		if number == math.Trunc(number) && number >= -float64(twoTo63) && number < float64(twoTo63) {
			return int64(number)
		}
		return number
	}

	s, ok := raw.(string)
	if !ok {
		return raw
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}

	if trimmed[0] == '{' || trimmed[0] == '[' {
		var out any
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			return out
		}
	}
	if trimmed[0] == '"' {
		var out string
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			return out
		}
	}

	switch trimmed {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}
	return s
}

func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}
