package runtime

import (
	"context"
	"slices"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Pick extracts a single field or filters the map with an allowlist / denylist.
var Pick = action.New("runtime.pick", func(_ context.Context, in any) (any, error) {
	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("pick: input must be an object")
	}

	// 1. Single field mode
	field := strings.TrimSpace(readStringArg(m, "field"))
	if field != "" {
		val, found := lookupDottedPath(m, field)
		if !found {
			return nil, xerr.NotFound("pick: field '" + field + "' not found")
		}
		return val, nil
	}

	// 2. Multi-field allowlist mode: only: ["a", "b"] or only: "a,b"
	if onlyVal, exists := m["only"]; exists {
		keys := parseKeyList(onlyVal)
		if len(keys) > 0 {
			filtered := make(map[string]any, len(keys))
			for _, k := range keys {
				if val, found := lookupDottedPath(m, k); found {
					filtered[k] = val
				}
			}
			return filtered, nil
		}
	}

	// 3. Denylist mode: drop: ["secret", "temp"] or drop: "secret,temp"
	if dropVal, exists := m["drop"]; exists {
		keys := parseKeyList(dropVal)
		if len(keys) > 0 {
			filtered := make(map[string]any, len(m))
			for k, v := range m {
				if k != "drop" && !slices.Contains(keys, k) {
					filtered[k] = v
				}
			}
			return filtered, nil
		}
	}

	return nil, xerr.BadRequest("pick: must specify 'field', 'only', or 'drop'")
}).Description("Extract field or apply allowlist/denylist to input keys").
	Tag("base", "shape").
	Build()

func parseKeyList(val any) []string {
	switch v := val.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
}
