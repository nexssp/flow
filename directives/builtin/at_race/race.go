package at_race

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

const declarationKey = "race"

type Decl struct {
	Pos     core.Position
	Name    string
	Members []string
}

type directive struct{}

func init() {
	d := directive{}
	core.Register(d)
	core.RegisterRegistryContributor(d)
}

func (directive) Name() string { return "race" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "race", err.Error())
	}

	name, err := parseName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "race", err.Error())
	}

	members, err := parseMembers(body, ctx, i, name)
	if err != nil {
		return 0, err
	}
	if len(members) < 2 {
		return 0, core.AtErrf(ctx, i, "race "+name,
			"needs at least 2 members, got %d", len(members))
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]Decl)
	for _, e := range existing {
		if e.Name == name {
			return 0, core.AtErrf(ctx, i, "race "+name,
				"duplicate declaration (already at %s)", e.Pos)
		}
	}
	ctx.Out.Declarations[declarationKey] = append(existing, Decl{
		Pos:     core.Position{File: ctx.File, Line: i + 1},
		Name:    name,
		Members: members,
	})

	return next, nil
}

func (directive) RegisterActions(pre *core.Preprocessed, base *action.Registry) ([]action.AnyAction, error) {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return nil, nil
	}
	decls, ok := raw.([]Decl)
	if !ok {
		return nil, fmt.Errorf("@race: declaration type %T not []Decl", raw)
	}

	out := make([]action.AnyAction, 0, len(decls))
	for _, decl := range decls {
		if base != nil {
			if _, exists := base.Get(decl.Name); exists {
				continue
			}
		}
		out = append(out, newRaceAction(decl))
	}
	return out, nil
}

func newRaceAction(decl Decl) action.AnyAction {
	members := append([]string(nil), decl.Members...)

	return action.New(decl.Name, func(ctx context.Context, input any) (any, error) {
		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal(fmt.Sprintf(
				"@race %q: no registry in context", decl.Name))
		}

		routes := make(map[string]action.AnyAction, len(members))
		for _, name := range members {
			target, ok := reg.Get(name)
			if !ok {
				return nil, xerr.NotFound(fmt.Sprintf(
					"@race %q: member %q is not in the registry",
					decl.Name, name))
			}
			routes[name] = target
		}

		return invokeRace(ctx, routes, input)
	}).
		Description(fmt.Sprintf("Race group %q (%d members)",
			decl.Name, len(members))).
		Tag("flow", "race", "fanout").
		Build()
}

func invokeRace(
	ctx context.Context,
	routes map[string]action.AnyAction,
	input any,
) (any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type outcome struct {
		name string
		val  any
		err  error
	}
	results := make(chan outcome, len(routes))
	var wg sync.WaitGroup

	for name, target := range routes {
		wg.Add(1)
		go func(n string, act action.AnyAction) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results <- outcome{name: n, err: xerr.PanicRecovery(r)}
				}
			}()
			val, err := action.InvokeAny(ctx, act, input)
			results <- outcome{name: n, val: val, err: err}
		}(name, target)
	}

	var (
		winner    any
		hasWinner bool
		lastErr   error
	)

	for range routes {
		out := <-results
		if out.err == nil && !hasWinner {
			winner = out.val
			hasWinner = true
			cancel() // Signal all losing members to abort immediately
			break
		}
		lastErr = out.err
	}

	cancel()
	// Wait for all losing goroutines to finish to prevent them from reading pooled context scope after return.
	wg.Wait()

	if hasWinner {
		return winner, nil
	}
	if lastErr == nil {
		lastErr = xerr.NotFound("race: no member succeeded")
	}
	return nil, lastErr
}

func parseName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "race")
	if !ok {
		return "", fmt.Errorf("expected `@race NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@race NAME { ... }`")
	}
	if err := validateName(name); err != nil {
		return "", err
	}
	return name, nil
}

func parseMembers(body []string, ctx *core.Context, baseLine int, name string) ([]string, error) {
	var members []string
	seen := make(map[string]bool)

	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" ||
			strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "//") {
			continue
		}
		bodyLine := baseLine + offset + 2

		if strings.ContainsAny(line, " \t") {
			return nil, core.AtErrf(ctx, bodyLine, "race "+name,
				"expected one action name per line, got %q", line)
		}
		if seen[line] {
			return nil, core.AtErrf(ctx, bodyLine, "race "+name,
				"duplicate member %q", line)
		}
		seen[line] = true
		members = append(members, line)
	}
	return members, nil
}

func validateName(s string) error {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 0) ||
			c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("invalid character %q in race name", string(c))
		}
	}
	return nil
}
