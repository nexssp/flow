package require

import (
	"context"
	"maps"
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

	opts, next, err := ParseDirectiveOptions(req.Lines, req.I, req.File)
	if err != nil {
		return compiler.DirectiveRes{}, err
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
	if req.Out == nil {
		return compiler.DirectiveRes{}, compiler.SourceError(
			compiler.Position{File: req.File, Line: req.I + 1},
			"@require: requirement metadata output is unavailable",
		)
	}
	var declarations []SourceDeclaration
	if raw, exists := req.Out[sourceDeclarationsKey]; exists {
		var ok bool
		declarations, ok = raw.([]SourceDeclaration)
		if !ok {
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1},
				"@require: invalid declaration metadata type %T", raw,
			)
		}
	}
	var existing []Requirement
	if raw, exists := req.Out["require"]; exists {
		var ok bool
		existing, ok = raw.([]Requirement)
		if !ok {
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1},
				"@require: invalid requirement metadata type %T", raw,
			)
		}
	}
	return recordRequirementMetadata(req, r, declaration, declarations, existing, next)
}

func recordRequirementMetadata(req compiler.DirectiveReq, requirement Requirement, declaration SourceDeclaration, declarations []SourceDeclaration, existing []Requirement, next int) (compiler.DirectiveRes, error) {
	for i := range existing {
		old := &existing[i]
		if old.Import == requirement.Import && old.Version == requirement.Version && old.Alias == requirement.Alias {
			previous, found := compiler.DeclarationPosition(req.Out, "require", requirementKey(old))
			if !found {
				return compiler.DirectiveRes{}, compiler.SourceError(
					compiler.Position{File: req.File, Line: req.I + 1},
					"@require: missing source position for existing declaration %q", requirement.Import,
				)
			}
			if !maps.Equal(old.Options, requirement.Options) {
				return compiler.DirectiveRes{}, compiler.SourceError(
					compiler.Position{File: req.File, Line: req.I + 1},
					"@require: duplicate declaration for %q (version %q, alias %q) has conflicting options; first declared at %s",
					requirement.Import, requirement.Version, requirement.Alias, previous,
				)
			}
			req.Out[sourceDeclarationsKey] = append(declarations, declaration)
			return compiler.DirectiveRes{Next: next}, nil
		}
		if requirement.Alias != "" && old.Alias == requirement.Alias {
			previous, found := compiler.DeclarationPosition(req.Out, "require", requirementKey(old))
			if !found {
				return compiler.DirectiveRes{}, compiler.SourceError(
					compiler.Position{File: req.File, Line: req.I + 1},
					"@require: missing source position for existing namespace qualifier %q", requirement.Alias,
				)
			}
			return compiler.DirectiveRes{}, compiler.SourceError(
				compiler.Position{File: req.File, Line: req.I + 1},
				"@require: namespace qualifier %q is already used for module %q; first declared at %s", requirement.Alias, old.Import, previous)
		}
	}
	if err := req.Declare("require", requirementKey(&requirement)); err != nil {
		return compiler.DirectiveRes{}, err
	}
	req.Out[sourceDeclarationsKey] = append(declarations, declaration)
	req.Out["require"] = append(existing, requirement)
	return compiler.DirectiveRes{Next: next}, nil
}

// ParseDirectiveOptions parses the optional object block on an @require line
// without resolving the requirement. It is shared by the directive handler and
// syntax-only consumers such as the formatter.
func ParseDirectiveOptions(lines []string, start int, file string) (options map[string]string, nextLine int, parseErr error) {
	if start < 0 || start >= len(lines) {
		return nil, start, compiler.SourceError(
			compiler.Position{File: file, Line: start + 1},
			"@require: missing directive line",
		)
	}
	line := lines[start]
	leading := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
	trimmed := line[leading:]
	if !strings.HasPrefix(trimmed, "@require") {
		return nil, start + 1, compiler.SourceError(
			compiler.Position{File: file, Line: start + 1},
			"@require: missing module path",
		)
	}
	headerStart := leading + len("@require")
	header := line[headerStart:]
	header = header[:len(stripRequirementComment(header))]
	openIdx := firstUnquotedBrace(header)
	opts := map[string]string{}
	next := start + 1
	if openIdx >= 0 {
		parsed, after, err := parseInlineOptions(line[headerStart+openIdx:], lines, start)
		if err != nil {
			return nil, start + 1, compiler.SourceError(
				compiler.Position{File: file, Line: start + 1},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	} else if start+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[start+1]), "{") {
		parsed, after, err := parseBlockOptions(lines, start+1)
		if err != nil {
			return nil, start + 1, compiler.SourceError(
				compiler.Position{File: file, Line: start + 2},
				"@require options: %v", err,
			)
		}
		opts = parsed
		next = after
	}
	return opts, next, nil
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
