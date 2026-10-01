package runtime

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Wrap wraps the input object under a named key. The key is required:
// wrap @{ key: "data" }.
var Wrap = action.New("wrap", func(_ context.Context, in any) (any, error) {
	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("wrap: input must be an object carrying the `key` arg")
	}
	key := strings.TrimSpace(readStringArg(m, "key"))
	if key == "" {
		return nil, xerr.BadRequest(`wrap: key is required (use: wrap @{ key: "data" })`)
	}
	inner := make(map[string]any, len(m))
	for k, v := range m {
		if k == "key" {
			continue
		}
		inner[k] = v
	}
	return map[string]any{key: inner}, nil
}).Description("Wrap the input object under a named key").
	Tag("base", "shape").
	Build()
