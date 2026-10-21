package require

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

var Directive = compiler.Directive{
	Name:     "require",
	Example:  "@require ./locallib as mylib\n@require github.com/nexssp/text-tools v1.0.0 as text_tools",
	Handler:  handleDirective,
	Metadata: metadataMerger(),
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	spec := strings.TrimSpace(strings.TrimPrefix(line, "@require"))
	if spec == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
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
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	} else if req.I+1 < len(req.Lines) && strings.HasPrefix(strings.TrimSpace(req.Lines[req.I+1]), "{") {
		parsed, after, err := parseBlockOptions(req.Lines, req.I+1)
		if err != nil {
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 2},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	}

	r, err := Parse(spec, req.BaseDir, opts, req.File, req.I+1)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}

	existing, _ := req.Out["require"].([]Requirement)
	for i := range existing {
		old := &existing[i]
		if old.Import == r.Import && old.Version == r.Version && old.Alias == r.Alias {
			return compiler.DirectiveRes{Next: next}, nil
		}
		if r.Alias != "" && old.Alias == r.Alias {
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1},
				"@require: namespace qualifier %q is already used for module %q", r.Alias, old.Import)
		}
	}
	req.Out["require"] = append(existing, r)
	return compiler.DirectiveRes{Next: next}, nil
}
