package at_route

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
)

const declarationKey = "route"

type RouteDecl struct {
	Pos   core.Position
	Name  string
	Whens []RouteWhen
	Else  string
}

type RouteWhen struct {
	Pos    core.Position
	Cond   string
	Target string
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterRegistryContributor(d)
}

func (directive) Name() string { return "route" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "route", err.Error())
	}

	name, err := parseRouteName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "route", err.Error())
	}

	decl := RouteDecl{
		Pos:  core.Position{File: ctx.File, Line: i + 1},
		Name: name,
	}

	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" ||
			strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "//") {
			continue
		}

		bodyLine := i + offset + 2

		switch {
		case strings.HasPrefix(line, "when "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "when "))
			arrow := strings.Index(rest, "->")
			if arrow < 0 {
				return 0, core.AtErrf(ctx, bodyLine, "route "+name,
					"expected `when COND -> TARGET`, got %q", line)
			}
			cond := strings.TrimSpace(rest[:arrow])
			target := strings.TrimSpace(rest[arrow+2:])
			if cond == "" || target == "" {
				return 0, core.AtErrf(ctx, bodyLine, "route "+name,
					"empty condition or target in %q", line)
			}
			decl.Whens = append(decl.Whens, RouteWhen{
				Pos:    core.Position{File: ctx.File, Line: bodyLine},
				Cond:   cond,
				Target: target,
			})

		case strings.HasPrefix(line, "else"):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "else"))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "->"))
			if rest == "" {
				return 0, core.AtErrf(ctx, bodyLine, "route "+name,
					"empty else target in %q", line)
			}
			if decl.Else != "" {
				return 0, core.AtErrf(ctx, bodyLine, "route "+name,
					"duplicate else clause (already set to %q)", decl.Else)
			}
			decl.Else = rest

		default:
			return 0, core.AtErrf(ctx, bodyLine, "route "+name,
				"unexpected line %q (expected `when ...` or `else ...`)", line)
		}
	}

	if len(decl.Whens) == 0 {
		return 0, core.AtErr(ctx, i, "route "+name,
			"needs at least one `when COND -> TARGET` clause")
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]RouteDecl)
	for _, e := range existing {
		if e.Name == name {
			return 0, core.AtErrf(ctx, i, "route "+name,
				"duplicate declaration (already at %s)", e.Pos)
		}
	}
	ctx.Out.Declarations[declarationKey] = append(existing, decl)

	return next, nil
}

// RegisterActions contributes route groups, skipping names that the
// caller's base registry already declares.
func (directive) RegisterActions(pre *core.Preprocessed, base *action.Registry) ([]action.AnyAction, error) {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return nil, nil
	}
	decls, ok := raw.([]RouteDecl)
	if !ok {
		return nil, fmt.Errorf("@route: declaration type %T not []RouteDecl", raw)
	}

	out := make([]action.AnyAction, 0, len(decls))
	for i := range decls {
		if base != nil {
			if _, exists := base.Get(decls[i].Name); exists {
				continue
			}
		}
		spec := RouteSpec{
			Name: decls[i].Name,
			Else: decls[i].Else,
		}
		spec.Whens = make([]RouteWhenSpec, len(decls[i].Whens))
		for j, w := range decls[i].Whens {
			spec.Whens[j] = RouteWhenSpec{Condition: w.Cond, Target: w.Target}
		}

		act, err := NewRouteAction(spec)
		if err != nil {
			return nil, fmt.Errorf("@route %q (declared at %s): %w",
				decls[i].Name, decls[i].Pos, err)
		}
		out = append(out, act)
	}
	return out, nil
}

func parseRouteName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "route")
	if !ok {
		return "", fmt.Errorf("expected `@route NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@route NAME { ... }`")
	}
	if err := validateRouteName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateRouteName(s string) error {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 0) ||
			c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("invalid character %q in route name", string(c))
		}
	}
	return nil
}
