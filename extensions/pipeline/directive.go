package pipeline

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@pipeline NAME ... @end` and stores the block body
// in meta["pipelines"][NAME]. Inline modifiers after the name are
// ignored.
var Directive = core.Directive{
	Name:    "pipeline",
	Example: "@pipeline pack\n  src -> dst\n@end",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	header := strings.TrimSpace(req.Lines[req.I])
	name := strings.TrimSpace(strings.TrimPrefix(header, "@pipeline"))
	if colon := strings.IndexByte(name, ':'); colon > 0 {
		name = strings.TrimSpace(name[:colon])
	}
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@pipeline requires a name",
		)
	}

	next := req.I + 1
	var bodyLines []string
	for next < len(req.Lines) {
		if strings.TrimSpace(req.Lines[next]) == "@end" {
			next++
			break
		}
		bodyLines = append(bodyLines, req.Lines[next])
		next++
	}

	pipelines, _ := req.Out["pipelines"].(map[string]string)
	if pipelines == nil {
		pipelines = make(map[string]string)
		req.Out["pipelines"] = pipelines
	}
	pipelines[name] = strings.Join(bodyLines, "\n")

	return core.DirectiveRes{Next: next}, nil
}
