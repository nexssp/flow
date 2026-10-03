// Package scope provides the `@scope :mods { ... }` directive. The
// header's modifiers become the effective policy for every atom
// declared inside the braces. Nested scopes override outer scopes on a
// per-modifier-name basis; an atom's own modifier always wins.
//
// Typical use:
//
//	@scope :timeout=5s :retry=3 {
//	  http.request
//	  json.parse
//	}
//
// The directive stores its line spans under the private meta key
// "scope.spans". core.Parser never sees these records; it consults a
// generic line-indexed modifier lookup that Bundle.OnPreprocess
// installs via core.WithLineModifiers.
package scope

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// metaKey is the private slot where this extension keeps its parsed
// @scope declarations. Nothing outside this package reads or writes it.
const metaKey = "scope.spans"

// span is one parsed @scope block: the line range covered by its body
// and the modifiers declared in its header.
type span struct {
	start     int
	end       int
	modifiers []string
}

func appendSpan(meta map[string]any, s span) {
	existing, _ := meta[metaKey].([]span)
	meta[metaKey] = append(existing, s)
}

func spansFromMeta(meta map[string]any) []span {
	s, _ := meta[metaKey].([]span)
	return s
}

// Directive parses `@scope :mods { ... }`.
var Directive = core.Directive{
	Name:    "scope",
	Example: "@scope :timeout=5s :retry=3 {\n  http.request\n}",
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	header, after, ok := strings.Cut(line, "{")
	if !ok {
		return core.DirectiveRes{}, core.SourceError(pos, "@scope requires an opening '{'")
	}

	if strings.TrimSpace(after) != "" {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@scope: body must start on the line after the opening '{'")
	}

	mods := parseScopeHeader(header)

	endLine := -1
	for j := req.I + 1; j < len(req.Lines); j++ {
		if strings.TrimSpace(req.Lines[j]) == "}" {
			endLine = j
			break
		}
	}
	if endLine < 0 {
		return core.DirectiveRes{}, core.SourceError(pos,
			"@scope: unclosed block, expected '}'")
	}

	appendSpan(req.Out, span{
		start:     req.I + 2,
		end:       endLine,
		modifiers: mods,
	})

	return core.DirectiveRes{
		Next:       req.I + 1,
		BlankLines: []int{req.I, endLine},
	}, nil
}

// parseScopeHeader extracts `:mod` and `:mod=value` tokens from the
// text before `{`.
func parseScopeHeader(header string) []string {
	header = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(header), "@scope"))
	var mods []string
	rest := header
	for rest != "" {
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			break
		}
		rest = rest[i+1:]

		key, remaining := readModKey(rest)
		rest = remaining
		if key == "" {
			continue
		}

		if rest == "" || rest[0] != '=' {
			mods = append(mods, key)
			rest = strings.TrimSpace(rest)
			continue
		}

		value, remaining := readModValue(rest[1:])
		rest = strings.TrimSpace(remaining)
		mods = append(mods, key+"="+value)
	}
	return mods
}

func readModKey(rest string) (key, remaining string) {
	i := strings.IndexAny(rest, "=: \t")
	if i < 0 {
		return strings.TrimSpace(rest), ""
	}
	return strings.TrimSpace(rest[:i]), rest[i:]
}

func readModValue(rest string) (value, remaining string) {
	if rest == "" {
		return "", ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		return readQuotedModValue(rest)
	}
	i := strings.IndexAny(rest, " \t:")
	if i < 0 {
		return rest, ""
	}
	return rest[:i], rest[i:]
}

func readQuotedModValue(rest string) (value, remaining string) {
	quote := rest[0]
	rest = rest[1:]
	var sb strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\\' && i+1 < len(rest) {
			sb.WriteByte(rest[i+1])
			i++
			continue
		}
		if c == quote {
			return sb.String(), rest[i+1:]
		}
		sb.WriteByte(c)
	}
	return sb.String(), ""
}
