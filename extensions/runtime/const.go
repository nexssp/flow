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

var Const = action.New("runtime.const", func(_ context.Context, in any) (any, error) {
	switch v := in.(type) {
	case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return coerceLiteral(v), nil
	}

	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("const: expected object or scalar, got " + typeName(in))
	}

	// 1. If explicit "value" or "val" was passed, unwrap it (even if upstream state merged into m)
	if raw, exists := m["value"]; exists {
		return coerceLiteral(raw), nil
	}
	if raw, exists := m["val"]; exists {
		return coerceLiteral(raw), nil
	}

	// 2. Object mode: const @{ server: "alpha", port: 8080 }
	if len(m) > 0 {
		clean := make(map[string]any, len(m))
		for k, v := range m {
			if k != "__root__" {
				clean[k] = v
			}
		}
		return clean, nil
	}

	return nil, xerr.BadRequest("const: missing required field 'value'")
}).Description("Return a fixed literal; numbers, bools, and JSON auto-parse").
	Tag("base", "literal").
	Build()

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
