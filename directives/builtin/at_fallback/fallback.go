package at_fallback

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
)

const declarationKey = "fallback"

// Decl is one parsed `@fallback NAME { ... }` declaration.
type Decl struct {
	Pos        core.Position
	ActionName string
	Rules      []core.RecoveryRule
	Else       string
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterAtomPolicy(d)
}

func (directive) Name() string { return "fallback" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "fallback", err.Error())
	}

	name, err := parseName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "fallback", err.Error())
	}

	rules, els, err := core.ParseRecoveryBlock(body, ctx, i, "fallback")
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 && els == "" {
		return 0, core.AtErr(ctx, i, "fallback "+name,
			"needs at least one `when ...` clause or an `else`")
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]Decl)
	for _, e := range existing {
		if e.ActionName == name {
			return 0, core.AtErrf(ctx, i, "fallback "+name,
				"duplicate declaration (already at %s)", e.Pos)
		}
	}
	ctx.Out.Declarations[declarationKey] = append(existing, Decl{
		Pos:        core.Position{File: ctx.File, Line: i + 1},
		ActionName: name,
		Rules:      rules,
		Else:       els,
	})

	return next, nil
}

// AtomAdvisor returns the fallback middleware for every atom whose name
// matches a declared @fallback. Installed with UseFirst so the
// middleware is outermost and observes failures from timeout, retry,
// cancellation, and the atom's own handler.
func (directive) AtomAdvisor(pre *core.Preprocessed) core.AtomAdvisor {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return nil
	}
	decls, ok := raw.([]Decl)
	if !ok || len(decls) == 0 {
		return nil
	}

	byName := make(map[string]Decl, len(decls))
	for _, d := range decls {
		byName[d.ActionName] = d
	}

	return func(atomName string, builder *action.Builder[any, any], _ []string) {
		decl, ok := byName[atomName]
		if !ok {
			return
		}
		builder.UseFirst(core.RecoveryMiddleware(core.RecoverySpec{
			Rules: decl.Rules,
			Else:  decl.Else,
		}))
	}
}

func parseName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "fallback")
	if !ok {
		return "", fmt.Errorf("expected `@fallback NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@fallback NAME { ... }`")
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateName(s string) error {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 0) ||
			c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("invalid character %q in fallback name", string(c))
		}
	}
	return nil
}
