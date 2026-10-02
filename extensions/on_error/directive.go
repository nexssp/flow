package on_error

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// Rule is a single conditional mapping in a @on_error block.
type Rule struct {
	Condition string
	Target    string
}

// Config is the parsed @on_error block.
type Config struct {
	Rules      []Rule
	ElseTarget string
}

// Directive parses the `@on_error { ... }` block and writes it to
// meta["on_error"].
var Directive = core.Directive{
	Name: "on_error",
	Example: `@on_error {
  when error.kind == "Timeout" -> cov.recoverable
  else -> runtime.noop
}`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	pos := core.Position{File: req.File, Line: req.I + 1}
	lines, next, err := core.ReadBlock(req.Lines, req.I)
	if err != nil {
		return core.DirectiveRes{}, core.SourceError(pos, "@on_error: %v", err)
	}

	cfg := Config{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "else") {
			_, target, ok := strings.Cut(line, "->")
			if !ok {
				return core.DirectiveRes{}, core.SourceError(pos, "invalid else clause: %q", line)
			}
			cfg.ElseTarget = strings.TrimSpace(target)
			continue
		}

		if after, ok := strings.CutPrefix(line, "when"); ok {
			rest := strings.TrimSpace(after)
			condition, target, ok := strings.Cut(rest, "->")
			if !ok {
				return core.DirectiveRes{}, core.SourceError(pos, "invalid when clause: %q", line)
			}
			cfg.Rules = append(cfg.Rules, Rule{
				Condition: strings.TrimSpace(condition),
				Target:    strings.TrimSpace(target),
			})
			continue
		}

		return core.DirectiveRes{}, core.SourceError(pos,
			"expected 'when <cond> -> <target>' or 'else -> <target>', got %q", line)
	}

	req.Out["on_error"] = cfg
	return core.DirectiveRes{Next: next}, nil
}
