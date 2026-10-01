package runtime

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Pick extracts one dotted-path field from the input object. The field
// is required: pick @{ field: "user.email" }.
var Pick = action.New("pick", func(_ context.Context, in any) (any, error) {
	m, ok := in.(map[string]any)
	if !ok {
		return nil, xerr.BadRequest("pick: input must be an object carrying the `field` arg")
	}
	field := strings.TrimSpace(readStringArg(m, "field"))
	if field == "" {
		return nil, xerr.BadRequest(`pick: field is required (use: pick @{ field: "user.name" })`)
	}
	value, ok := lookupDottedPath(m, field)
	if !ok {
		return nil, xerr.NotFound("pick: field " + field + " not found")
	}
	return value, nil
}).Description("Extract one dotted-path field from the input object").
	Tag("base", "shape").
	Build()
