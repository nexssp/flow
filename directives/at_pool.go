package directives

import (
	"fmt"
	"strings"
)

type atPool struct{}

func init() { Register(atPool{}) }

func (atPool) Name() string { return "pool" }

// Syntax:
//
//	@pool experts [fast, smart, local]
//
// Every member must refer to a name that exists at dispatch time: an
// action registered in Go, an action synthesised by a domain directive
// (an @llm NAME becomes an action named NAME), or another pool name.
//
// The directive itself does not resolve members. It records the
// declaration, and the dispatch action resolves each member against
// the registry when it is called.
func (atPool) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	rest, ok := StripDirectivePrefix(line, "pool")
	if !ok {
		return 0, AtErr(ctx, i, "pool", "malformed directive")
	}

	name, members, err := splitPoolDecl(rest)
	if err != nil {
		return 0, AtErr(ctx, i, "pool", err.Error())
	}
	if name == "" {
		return 0, AtErr(ctx, i, "pool",
			"missing name, expected @pool NAME [m1, m2]")
	}
	if err := validatePoolName(name); err != nil {
		return 0, AtErr(ctx, i, "pool", err.Error())
	}
	if len(members) == 0 {
		return 0, AtErr(ctx, i, "pool "+name, "at least one member required")
	}

	for _, p := range ctx.Out.Pools {
		if p.Name == name {
			return 0, AtErrf(ctx, i, "pool "+name,
				"duplicate declaration (already at %s)", p.Pos.String())
		}
	}

	ctx.Out.Pools = append(ctx.Out.Pools, Pool{
		Name:    name,
		Members: members,
		Pos:     Position{File: ctx.File, Line: i + 1},
	})

	return i + 1, nil
}

// splitPoolDecl parses "experts [fast, smart, local]" into
// ("experts", ["fast","smart","local"]).
func splitPoolDecl(s string) (string, []string, error) {
	s = strings.TrimSpace(s)

	open := strings.IndexByte(s, '[')
	if open < 0 {
		return "", nil, fmt.Errorf("expected `NAME [m1, m2]`, got %q", s)
	}

	name := strings.TrimSpace(s[:open])
	members := ParseList(s[open:])
	if name == "" {
		return "", nil, fmt.Errorf("missing name before '['")
	}
	return name, members, nil
}

func validatePoolName(s string) error {
	if s == "" {
		return fmt.Errorf("name cannot be empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 0) ||
			c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("invalid character %q in name", string(c))
		}
	}
	return nil
}
