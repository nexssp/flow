package scope

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// ProfileDirective declares a named, reusable set of modifiers:
//
//	@profile reliable :timeout=2s :retry=4
//
// Profiles are stored under meta["scope.profiles"]. A profile must be
// declared before the @pipeline or @scope that references it.
var ProfileDirective = core.Directive{
	Name:    "profile",
	Example: "@profile reliable :timeout=2s :retry=4",
	Handler: handleProfile,
}

func handleProfile(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	rest := strings.TrimSpace(strings.TrimPrefix(line, "@profile"))
	if rest == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@profile requires a name")
	}

	// Name is up to first ':' or whitespace.
	nameEnd := strings.IndexAny(rest, ": \t")
	var name, tail string
	if nameEnd < 0 {
		name = rest
	} else {
		name = strings.TrimSpace(rest[:nameEnd])
		tail = rest[nameEnd:]
	}
	if name == "" {
		return core.DirectiveRes{}, core.SourceError(pos, "@profile: missing name")
	}
	if !isValidProfileName(name) {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@profile: invalid name %q", name)
	}

	mods := parseScopeHeader(": " + strings.TrimSpace(tail))

	profiles, _ := req.Out["scope.profiles"].(map[string][]string)
	if profiles == nil {
		profiles = make(map[string][]string)
		req.Out["scope.profiles"] = profiles
	}
	if _, dup := profiles[name]; dup {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@profile %s: duplicate declaration", name)
	}
	profiles[name] = mods

	return core.DirectiveRes{Next: req.I + 1}, nil
}

func isValidProfileName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] != '_' && !isASCIILetter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c != '_' && !isASCIILetter(c) && !isASCIIDigit(c) {
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
