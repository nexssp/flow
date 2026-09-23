package at_approval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
)

const declarationKey = "approval"

// Decl is one parsed `@approval NAME { ... }` declaration.
type Decl struct {
	Pos     core.Position
	Name    string
	Role    string
	Reason  string
	Expires time.Duration
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterRegistryContributor(d)
}

func (directive) Name() string { return "approval" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "approval", err.Error())
	}

	name, err := parseName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "approval", err.Error())
	}

	decl := Decl{
		Pos:     core.Position{File: ctx.File, Line: i + 1},
		Name:    name,
		Expires: 30 * time.Minute,
	}

	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" ||
			strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "//") {
			continue
		}
		bodyLine := i + offset + 2

		key, value, ok := splitField(line)
		if !ok {
			return 0, core.AtErrf(ctx, bodyLine, "approval "+name,
				"expected `key: value`, got %q", line)
		}
		if err := applyField(&decl, key, value, ctx, bodyLine, name); err != nil {
			return 0, err
		}
	}

	if decl.Role == "" {
		return 0, core.AtErr(ctx, i, "approval "+name,
			"`role` is required")
	}
	if decl.Reason == "" {
		return 0, core.AtErr(ctx, i, "approval "+name,
			"`reason` is required")
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]Decl)
	for _, e := range existing {
		if e.Name == name {
			return 0, core.AtErrf(ctx, i, "approval "+name,
				"duplicate declaration (already at %s)", e.Pos)
		}
	}
	ctx.Out.Declarations[declarationKey] = append(existing, decl)

	return next, nil
}

// RegisterActions turns every parsed @approval into a callable gate
// action named `approval.<name>`.
func (directive) RegisterActions(pre *core.Preprocessed, _ *action.Registry) ([]action.AnyAction, error) {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return nil, nil
	}
	decls, ok := raw.([]Decl)
	if !ok {
		return nil, fmt.Errorf("@approval: declaration type %T not []Decl", raw)
	}

	out := make([]action.AnyAction, 0, len(decls))
	for _, decl := range decls {
		out = append(out, newGateAction(decl))
	}
	return out, nil
}

func newGateAction(decl Decl) action.AnyAction {
	return action.New("approval."+decl.Name, func(ctx context.Context, input any) (any, error) {
		if approved(ctx, decl.Name) {
			return input, nil
		}
		return nil, dag.Suspend(decl.Reason, map[string]any{
			"gate":    decl.Name,
			"role":    decl.Role,
			"reason":  decl.Reason,
			"expires": decl.Expires.String(),
		})
	}).
		Description(fmt.Sprintf("Approval gate %q (role=%s)",
			decl.Name, decl.Role)).
		Tag("flow", "approval", "gate").
		Build()
}

// approved reports whether the current execution context carries an
// approval token that covers this gate.
//
// Token grammar: either the literal "all", or a comma-separated list
// of gate names. Whitespace around names is ignored. Comparison is
// case-sensitive.
func approved(ctx context.Context, gate string) bool {
	token := xctx.ApprovalTokenFrom(ctx)
	if token == "" {
		return false
	}
	if token == "all" {
		return true
	}
	for _, t := range strings.Split(token, ",") {
		if strings.TrimSpace(t) == gate {
			return true
		}
	}
	return false
}

func parseName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "approval")
	if !ok {
		return "", fmt.Errorf("expected `@approval NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@approval NAME { ... }`")
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
			return fmt.Errorf("invalid character %q in approval name", string(c))
		}
	}
	return nil
}

func splitField(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func applyField(decl *Decl, key, value string, ctx *core.Context, bodyLine int, name string) error {
	// Strip trailing comma if present
	value = strings.TrimSuffix(strings.TrimSpace(value), ",")

	switch key {
	case "role":
		v := strings.Trim(strings.TrimSpace(value), `"'`)
		if v == "" {
			return core.AtErrf(ctx, bodyLine, "approval "+name,
				"role cannot be empty")
		}
		decl.Role = v

	case "reason":
		v := strings.Trim(strings.TrimSpace(value), `"'`)
		if v == "" {
			return core.AtErrf(ctx, bodyLine, "approval "+name,
				"reason cannot be empty")
		}
		decl.Reason = v

	case "expires":
		d, err := time.ParseDuration(strings.Trim(value, `"'`))
		if err != nil {
			return core.AtErrf(ctx, bodyLine, "approval "+name,
				"expires: %v", err)
		}
		if d <= 0 {
			return core.AtErrf(ctx, bodyLine, "approval "+name,
				"expires must be > 0")
		}
		decl.Expires = d

	default:
		return core.AtErrf(ctx, bodyLine, "approval "+name,
			"unknown field %q (role|reason|expires)", key)
	}
	return nil
}
