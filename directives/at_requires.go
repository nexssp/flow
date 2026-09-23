package directives

import (
	"strings"

	"github.com/nexssp/flow/directives/core"
)

type atRequires struct{}

func init() { Register(atRequires{}) }

func (atRequires) Name() string { return "requires" }

// Syntax:
//
//	@requires: llm[openai,deepseek], sandbox[golang:1.26], network[api.github.com]
//
// The capability envelope is flow-level (not domain-level): the
// compiler uses it to reject undeclared capabilities at compile time,
// without knowing which domain owns any particular capability name.
//
// Multiple @requires lines are merged. Duplicate entries in the same
// domain are deduplicated silently.
func (atRequires) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	spec, ok := StripDirectivePrefix(line, "requires")
	if !ok {
		return 0, AtErr(ctx, i, "requires", "malformed directive")
	}
	if spec == "" {
		return 0, AtErr(ctx, i, "requires", "requires at least one capability")
	}

	for _, entry := range core.SplitTopLevel(spec, ',') {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		open := strings.IndexByte(entry, '[')
		if open < 0 {
			return 0, AtErrf(ctx, i, "requires",
				"expected `name[...]`, got %q", entry)
		}
		closeIdx := strings.LastIndexByte(entry, ']')
		if closeIdx < open {
			return 0, AtErrf(ctx, i, "requires",
				"missing ']' in %q", entry)
		}

		domain := strings.TrimSpace(entry[:open])
		if domain == "" {
			return 0, AtErrf(ctx, i, "requires",
				"empty capability domain in %q", entry)
		}

		values := ParseList(entry[open : closeIdx+1])
		if len(values) == 0 {
			return 0, AtErrf(ctx, i, "requires",
				"empty value list for %q", domain)
		}

		switch domain {
		case "llm":
			ctx.Out.Capabilities.LLM = appendUnique(ctx.Out.Capabilities.LLM, values...)
		case "sandbox":
			ctx.Out.Capabilities.Sandbox = appendUnique(ctx.Out.Capabilities.Sandbox, values...)
		case "network":
			ctx.Out.Capabilities.Network = appendUnique(ctx.Out.Capabilities.Network, values...)
		default:
			return 0, AtErrf(ctx, i, "requires",
				"unknown capability domain %q (known: llm, sandbox, network)", domain)
		}
	}

	ctx.Out.Capabilities.Pos = Position{File: ctx.File, Line: i + 1}
	return i + 1, nil
}

func appendUnique(dst []string, values ...string) []string {
outer:
	for _, v := range values {
		for _, existing := range dst {
			if existing == v {
				continue outer
			}
		}
		dst = append(dst, v)
	}
	return dst
}
