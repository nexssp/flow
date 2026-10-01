// Package description provides the `@description "..."` directive,
// which records a human-readable summary of a .nflow file into
// meta["description"]. The runtime ignores it; it is read by
// `nflow info` and by the selftest fixture discovery (a fixture's
// @description becomes its feature name).
//
// Typical use:
//
//	@description "Extract and summarize customer feedback"
package description

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "description"

var directive = core.Directive{
	Name:    "description",
	Example: `@description "hello"`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	text := strings.TrimSpace(strings.TrimPrefix(line, "@description"))
	req.Out["description"] = strings.Trim(text, `"'`)
	return core.DirectiveRes{Next: req.I + 1}, nil
}

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:         ID,
		Libraries:  []action.Library{{Name: ID}},
		Directives: []core.Directive{directive},
	}
}
