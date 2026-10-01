package require

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

var Directive = core.Directive{
	Name:    "require",
	Example: "@require ./locallib as mylib\n@require github.com/nexssp/text-tools v1.0.0 as text_tools",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	spec := strings.TrimSpace(strings.TrimPrefix(line, "@require"))
	if spec == "" {
		return core.DirectiveRes{}, core.SourceError(
			core.Position{File: req.File, Line: req.I + 1},
			"@require: missing module path",
		)
	}

	opts := map[string]string{}
	next := req.I + 1

	if openIdx := strings.Index(spec, "{"); openIdx >= 0 {
		inline := spec[openIdx:]
		spec = strings.TrimSpace(spec[:openIdx])

		parsed, after, err := parseInlineOptions(inline, req.Lines, req.I)
		if err != nil {
			return core.DirectiveRes{}, core.SourceError(
				core.Position{File: req.File, Line: req.I + 1},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	} else if req.I+1 < len(req.Lines) && strings.HasPrefix(strings.TrimSpace(req.Lines[req.I+1]), "{") {
		parsed, after, err := parseBlockOptions(req.Lines, req.I+1)
		if err != nil {
			return core.DirectiveRes{}, core.SourceError(
				core.Position{File: req.File, Line: req.I + 2},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	}

	r, err := Parse(spec, req.BaseDir, opts, req.File, req.I+1)
	if err != nil {
		return core.DirectiveRes{}, err
	}

	existing, _ := req.Out["require"].([]Requirement)
	for i := range existing {
		old := &existing[i]
		if old.Import == r.Import && old.Version == r.Version && old.Alias == r.Alias {
			return core.DirectiveRes{Next: next}, nil
		}
	}
	req.Out["require"] = append(existing, r)
	return core.DirectiveRes{Next: next}, nil
}
