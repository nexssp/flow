package directives

import (
	"strings"

	"github.com/nexssp/flow/directives/core"
)

// atHook implements the `@hook:` directive.
//
// Syntax:
//
//	@hook:name
//	@hook:name1,name2
//	@hook:[name1, name2]
//
// Every name is appended to ctx.Out.Hooks. Names are not resolved
// here — resolution happens at compile time in the runner, where the
// kernel hook registry is available. This keeps the directive
// side-effect-free and easy to test in isolation.
//
// Duplicate names are permitted: a caller may legitimately want the
// same hook attached to the whole pipeline and also to a specific
// atom. The runner preserves source order.
type atHook struct{}

func init() { Register(atHook{}) }

func (atHook) Name() string { return "hook" }

func (atHook) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	spec, ok := StripDirectivePrefix(line, "hook")
	if !ok {
		return 0, AtErr(ctx, i, "hook", "malformed directive")
	}

	names := ParseList(spec)
	if len(names) == 0 {
		return 0, AtErr(ctx, i, "hook", "requires at least one hook name")
	}

	pos := Position{File: ctx.File, Line: i + 1}

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ctx.Out.Hooks = append(ctx.Out.Hooks, core.HookDecl{
			Name: name,
			Pos:  pos,
		})
	}

	return i + 1, nil
}
