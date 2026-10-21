package pool

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// Directive parses `@pool NAME [members] { options }` and appends a
// Declaration to meta["pools"].
var Directive = compiler.Directive{
	Name:    "pool",
	Example: `@pool fast [cheap, mid] { strategy: "round_robin" }`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@pool"))
	pos := compiler.Position{File: req.File, Line: req.I + 1}

	openBracket := strings.IndexByte(rest, '[')
	switch {
	case openBracket < 0:
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@pool: expected `NAME [member1, member2]`")
	case openBracket == 0:
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"@pool: name is required before '['")
	}

	name := strings.TrimSpace(rest[:openBracket])

	members, err := compiler.ParseCapabilityRefList(rest)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@pool %s: %v", name, err)
	}

	var options map[string]string
	next := req.I + 1
	if strings.Contains(rest, "{") {
		parsed, after, err := compiler.ParseBlockOptions(req.Lines, req.I)
		if err != nil {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@pool %s: %v", name, err)
		}
		options = parsed
		next = after
	}

	if err := req.Declare("pool", name); err != nil {
		return compiler.DirectiveRes{}, err
	}
	for _, existing := range DeclarationsFromMeta(req.Out) {
		if existing.Name == name {
			return compiler.DirectiveRes{}, compiler.SourceError(pos, "@pool %s: duplicate declaration", name)
		}
	}

	req.Out["pools"] = append(DeclarationsFromMeta(req.Out), Declaration{
		Name:     name,
		Members:  members,
		Strategy: options["strategy"],
		Options:  options,
		Position: pos,
	})
	return compiler.DirectiveRes{Next: next}, nil
}
