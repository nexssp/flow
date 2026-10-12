package hook

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive parses `@hook:name1,name2` and appends every name to
// meta["hooks"]. WrapPipeline consumes the list.
var Directive = compiler.Directive{
	Name:    "hook",
	Example: "@hook:hook.verify",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@hook"))
	rest = strings.TrimPrefix(rest, ":")

	if rest == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@hook requires at least one hook name (e.g. @hook:hook.verify)",
		)
	}

	raw := compiler.SplitTopLevel(rest, ',')
	names := make([]string, 0, len(raw))
	for _, token := range raw {
		trimmed := strings.Trim(strings.TrimSpace(token), `"'[]`)
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}

	existing, _ := req.Out["hooks"].([]string)
	req.Out["hooks"] = append(existing, names...)
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}
