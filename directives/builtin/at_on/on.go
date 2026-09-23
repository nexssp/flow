// file: flow/directives/builtin/at_on/on.go
package at_on

import (
	"strings"

	"github.com/nexssp/flow/directives/core"
)

const DeclarationKey = "on_event"

type EventTrigger struct {
	Protocol string
	Target   string
	Pos      core.Position
}

type directive struct{}

func init() {
	core.Register(directive{})
}

func (directive) Name() string { return "on" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	rest, ok := core.StripDirectivePrefix(line, "on")
	if !ok {
		return 0, core.AtErr(ctx, i, "on", "malformed directive")
	}

	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "event") {
		return 0, core.AtErrf(ctx, i, "on", "expected `event \"protocol:target\"`, got %q", rest)
	}

	rawTarget := strings.TrimSpace(strings.TrimPrefix(rest, "event"))
	rawTarget = core.TrimQuotes(rawTarget)
	if rawTarget == "" {
		return 0, core.AtErr(ctx, i, "on", "event target is required")
	}

	colonIndex := strings.IndexByte(rawTarget, ':')
	if colonIndex <= 0 || colonIndex == len(rawTarget)-1 {
		return 0, core.AtErrf(ctx, i, "on", "expected \"protocol:target\", got %q", rawTarget)
	}

	protocol := strings.TrimSpace(rawTarget[:colonIndex])
	target := strings.TrimSpace(rawTarget[colonIndex+1:])

	trigger := EventTrigger{
		Protocol: protocol,
		Target:   target,
		Pos:      core.Position{File: ctx.File, Line: i + 1},
	}

	if ctx.Out.Declarations == nil {
		ctx.Out.Declarations = make(map[string]any)
	}
	ctx.Out.Declarations[DeclarationKey] = trigger

	return i + 1, nil
}
