package directives

import "strings"

type atAction struct{}

func init() { Register(atAction{}) }

func (atAction) Name() string { return "action" }

// Syntax:
//
//	@action users.create
//
// Declares the file's canonical name when it is registered as an
// action via flows.ScanFlowsFolder. At most one @action per file.
func (atAction) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	name, ok := StripDirectivePrefix(line, "action")
	if !ok {
		return 0, AtErr(ctx, i, "action", "malformed directive")
	}
	name = TrimQuotes(name)
	if name == "" {
		return 0, AtErr(ctx, i, "action", "requires a name")
	}

	if ctx.Out.Action == nil {
		ctx.Out.Action = &ActionMeta{Pos: Position{File: ctx.File, Line: i + 1}}
	}
	if ctx.Out.Action.Name != "" && ctx.Out.Action.Name != name {
		return 0, AtErrf(ctx, i, "action "+name,
			"already declared as %s", ctx.Out.Action.Name)
	}
	ctx.Out.Action.Name = name

	return i + 1, nil
}
