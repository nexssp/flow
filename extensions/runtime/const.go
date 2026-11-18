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

	"github.com/nexssp/flow/internal/constinput"
)

var Const = action.New("runtime.const", func(_ context.Context, in any) (any, error) {
	switch v := in.(type) {
	case constinput.Direct:
		if value, ok := v.Value.(string); ok {
			return value, nil
		}
		return removeConstStringMarkers(coerceLiteral(v.Value)), nil
	case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return coerceLiteral(v), nil
	}

	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("const: expected object or scalar, got " + typeName(in))
	}

	// 1. If explicit "value" or "val" was passed, unwrap it (even if upstream state merged into m)
	coerceRequested, _ := m["coerce"].(bool)
	if raw, exists := m["value"]; exists {
		return constValue(raw, coerceRequested), nil
	}
	if raw, exists := m["val"]; exists {
		return constValue(raw, coerceRequested), nil
	}

	// 2. Object mode: const(server: "alpha", port: 8080)
	if len(m) > 0 {
		clean := make(map[string]any, len(m))
		for k, v := range m {
			if k == "__root__" {
				continue
			}
			if value, ok := v.(constinput.String); ok {
				v = string(value)
			}
			clean[k] = v
		}
		return clean, nil
	}

	return nil, xerr.BadRequest("const: missing required field 'value'")
}).Description("Return a fixed scalar or named object; legacy value strings are auto-parsed").
	Tag("base", "literal").
	Build()

func constValue(raw any, coerceRequested bool) any {
	if value, ok := raw.(constinput.String); ok {
		if !coerceRequested {
			return string(value)
		}
		return coerceLiteral(string(value))
	}
	return coerceLiteral(raw)
}

func removeConstStringMarkers(value any) any {
	switch v := value.(type) {
	case constinput.String:
		return string(v)
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, item := range v {
			clean[key] = removeConstStringMarkers(item)
		}
		return clean
	case []any:
		clean := make([]any, len(v))
		for i, item := range v {
			clean[i] = removeConstStringMarkers(item)
		}
		return clean
	default:
		return value
	}
}

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
