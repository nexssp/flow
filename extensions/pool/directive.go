package pool

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// Directive parses `@pool NAME [members] { options }` and appends a
// Declaration to meta["pools"].
var Directive = core.Directive{
	Name:    "pool",
	Example: `@pool fast [cheap, mid] { strategy: "round_robin" }`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	line := strings.TrimSpace(req.Lines[req.I])
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@pool"))
	pos := core.Position{File: req.File, Line: req.I + 1}

	openBracket := strings.IndexByte(rest, '[')
	switch {
	case openBracket < 0:
		return core.DirectiveRes{}, core.SourceError(pos,
			"@pool: expected `NAME [member1, member2]`")
	case openBracket == 0:
		return core.DirectiveRes{}, core.SourceError(pos,
			"@pool: name is required before '['")
	}

	name := strings.TrimSpace(rest[:openBracket])

	members := core.ParseList(rest)
	if len(members) == 0 {
		return core.DirectiveRes{}, core.SourceError(pos, "@pool %s: member list is empty", name)
	}

	var options map[string]string
	next := req.I + 1
	if strings.Contains(rest, "{") {
		parsed, after, err := core.ParseBlockOptions(req.Lines, req.I)
		if err != nil {
			return core.DirectiveRes{}, core.SourceError(pos, "@pool %s: %v", name, err)
		}
		options = parsed
		next = after
	}

	for _, existing := range DeclarationsFromMeta(req.Out) {
		if existing.Name == name {
			return core.DirectiveRes{}, core.SourceError(pos, "@pool %s: duplicate declaration", name)
		}
	}

	req.Out["pools"] = append(DeclarationsFromMeta(req.Out), Declaration{
		Name:     name,
		Members:  members,
		Strategy: options["strategy"],
		Options:  options,
		Position: pos,
	})
	return core.DirectiveRes{Next: next}, nil
}
