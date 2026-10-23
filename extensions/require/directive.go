package require

import (
	"context"
	"strings"
	"unicode"

	compiler "github.com/nexssp/flow/compiler"
)

var Directive = compiler.Directive{
	Name:    "require",
	Example: "@require ./locallib as mylib\n@require github.com/nexssp/text-tools v1.0.0 as text_tools",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := req.Lines[req.I]
	leading := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
	trimmed := line[leading:]
	if !strings.HasPrefix(trimmed, "@require") {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@require: missing module path",
		)
	}
	headerStart := leading + len("@require")
	header := line[headerStart:]
	commentEnd := len(stripRequirementComment(header))
	header = header[:commentEnd]
	openIdx := firstUnquotedBrace(header)
	specEnd := len(header)
	if openIdx >= 0 {
		specEnd = openIdx
	}
	specRaw := header[:specEnd]
	specLeading := len(specRaw) - len(strings.TrimLeftFunc(specRaw, unicode.IsSpace))
	spec := specRaw[specLeading:]
	specBase := headerStart + specLeading
	if strings.TrimSpace(spec) == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@require: missing module path",
		)
	}

	opts := map[string]string{}
	next := req.I + 1

	if openIdx >= 0 {
		inline := line[headerStart+openIdx:]
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

	r, selectorStart, selectorEnd, err := parseRequirement(spec, req.BaseDir, opts, req.File, req.I+1)
	if err != nil {
		return compiler.DirectiveRes{}, err
	}
	declaration := SourceDeclaration{
		SourceLine:    line,
		Requirement:   r,
		Line:          req.I + 1,
		SelectorStart: specBase + selectorStart,
		SelectorEnd:   specBase + selectorEnd,
	}
	req.Out[sourceDeclarationsKey] = append(sourceDeclarationList(req.Out), declaration)

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
	if declareErr := req.Declare("require", requirementKey(&r)); declareErr != nil {
		return compiler.DirectiveRes{}, declareErr
	}
	req.Out["require"] = append(existing, r)
	return compiler.DirectiveRes{Next: next}, nil
}

func sourceDeclarationList(meta map[string]any) []SourceDeclaration {
	declarations, _ := meta[sourceDeclarationsKey].([]SourceDeclaration)
	return declarations
}

func firstUnquotedBrace(text string) int {
	var quote byte
	for i := range len(text) {
		c := text[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '{' {
			return i
		}
	}
	return -1
}
