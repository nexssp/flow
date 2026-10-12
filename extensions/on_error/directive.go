package on_error

import (
	"context"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

type Rule struct {
	Condition string
	Target    string
}

type Config struct {
	Rules      []Rule
	ElseTarget string
}

var Directive = compiler.Directive{
	Name: "on_error",
	Example: `@on_error {
  when error.kind == "Timeout" -> cov.recoverable
  else -> runtime.noop
}`,
	Handler: handleDirective,
}

func handleDirective(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	// NEW: short form — `@on_error target` or `@on_error:target`.
	//
	// When the rest of the declaration line does not begin with `{`,
	// treat it as a bare target action name and record it as the sole
	// (catch-all) recovery. This is exactly equivalent to
	//
	//     @on_error {
	//       else -> target
	//     }
	//
	// The target is resolved at runtime through the action resolver,
	// so it may name a pipeline (`pipeline.recover`), a bundled action
	// (`error.passthrough`), or any other registered capability.
	rest := strings.TrimSpace(strings.TrimPrefix(line, "@on_error"))
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimSpace(rest)

	if !strings.HasPrefix(rest, "{") {
		target := strings.TrimSpace(compiler.StripExprComments(rest))
		if target == "" {
			return compiler.DirectiveRes{}, compiler.SourceError(pos,
				"@on_error: expected `{ ... }` block or a target action name")
		}
		req.Out["on_error"] = Config{ElseTarget: target}
		return compiler.DirectiveRes{Next: req.I + 1}, nil
	}

	// Long form: unchanged.
	lines, next, err := compiler.ReadBlock(req.Lines, req.I)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@on_error: %v", err)
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
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "invalid else clause: %q", line)
			}
			cfg.ElseTarget = strings.TrimSpace(compiler.StripExprComments(target))
			continue
		}

		if after, ok := strings.CutPrefix(line, "when"); ok {
			rest := strings.TrimSpace(after)
			condition, target, ok := strings.Cut(rest, "->")
			if !ok {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "invalid when clause: %q", line)
			}
			condition = strings.TrimSpace(compiler.StripExprComments(condition))
			target = strings.TrimSpace(compiler.StripExprComments(target))
			cfg.Rules = append(cfg.Rules, Rule{Condition: condition, Target: target})
			continue
		}

		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			"expected 'when <cond> -> <target>' or 'else -> <target>', got %q", line)
	}

	req.Out["on_error"] = cfg
	return compiler.DirectiveRes{Next: next}, nil
}
