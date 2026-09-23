package at_retry

import (
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
)

// declarationKey is the slot under Preprocessed.Declarations that
// holds the retry declarations for this file.
const declarationKey = "retry"

// Decl is one parsed `@retry NAME { ... }` declaration.
type Decl struct {
	Pos         core.Position
	ActionName  string
	MaxAttempts int
	BackoffKind string
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Jitter      bool
	Only        []string
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterAtomPolicy(d)
}

func (directive) Name() string { return "retry" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "retry", err.Error())
	}

	name, err := parseName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "retry", err.Error())
	}

	decl := Decl{
		Pos:         core.Position{File: ctx.File, Line: i + 1},
		ActionName:  name,
		BackoffKind: "exponential",
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
			return 0, core.AtErrf(ctx, bodyLine, "retry "+name,
				"expected `key: value`, got %q", line)
		}
		if err := applyField(&decl, key, value, ctx, bodyLine, name); err != nil {
			return 0, err
		}
	}

	if decl.MaxAttempts <= 0 {
		return 0, core.AtErrf(ctx, i, "retry "+name,
			"`attempts` is required and must be > 0")
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]Decl)
	for _, e := range existing {
		if e.ActionName == name {
			return 0, core.AtErrf(ctx, i, "retry "+name,
				"duplicate declaration (already at %s)", e.Pos)
		}
	}
	ctx.Out.Declarations[declarationKey] = append(existing, decl)

	return next, nil
}

// AtomAdvisor returns the retry attach closure for every atom whose
// name matches a declared @retry. Atoms that already carry a local
// :retry= or :retry_if= modifier are skipped — the local form wins.
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

	return func(atomName string, builder *action.Builder[any, any], modifiers []string) {
		decl, ok := byName[atomName]
		if !ok {
			return
		}
		if hasLocalRetryModifier(modifiers) {
			return
		}
		applyRetryPolicy(builder, decl)
	}
}

func hasLocalRetryModifier(modifiers []string) bool {
	for _, raw := range modifiers {
		key := raw
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			key = key[:eq]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "retry" || key == "retry_if" {
			return true
		}
	}
	return false
}

func parseName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "retry")
	if !ok {
		return "", fmt.Errorf("expected `@retry NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@retry NAME { ... }`")
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
			return fmt.Errorf("invalid character %q in retry name", string(c))
		}
	}
	return nil
}
