package at_parallel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

const declarationKey = "parallel"

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

func (directive) Name() string { return "parallel" }

func (directive) Apply(ctx *core.Context, lines []string, i int) (int, error) {
	header, body, next, err := core.SplitBlock(lines, i)
	if err != nil {
		return 0, core.AtErr(ctx, i, "parallel", err.Error())
	}

	name, err := parseName(header)
	if err != nil {
		return 0, core.AtErr(ctx, i, "parallel", err.Error())
	}

	members, err := parseMembers(body, ctx, i, name)
	if err != nil {
		return 0, err
	}
	if len(members) < 2 {
		return 0, core.AtErrf(ctx, i, "parallel "+name,
			"needs at least 2 members, got %d", len(members))
	}

	existing, _ := ctx.Out.Declarations[declarationKey].([]Decl)
	for _, e := range existing {
		if e.Name == name {
			return 0, core.AtErrf(ctx, i, "parallel "+name,
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

// RegisterActions contributes the parallel group as a Kernel action.
//
// The base registry passed in by the caller may already contain an
// action with the same name — that happens when a test harness or an
// application pre-registers a stub for its own purposes. In that case
// the pre-existing action wins and the directive does not contribute
// anything. This keeps directive execution deterministic regardless of
// whether the caller's base registry happens to shadow a directive
// name.
func (directive) RegisterActions(pre *core.Preprocessed, base *action.Registry) ([]action.AnyAction, error) {
	raw, ok := pre.Declarations[declarationKey]
	if !ok {
		return nil, nil
	}
	decls, ok := raw.([]Decl)
	if !ok {
		return nil, fmt.Errorf("@parallel: declaration type %T not []Decl", raw)
	}

	out := make([]action.AnyAction, 0, len(decls))
	for _, decl := range decls {
		if base != nil {
			if _, exists := base.Get(decl.Name); exists {
				continue
			}
		}
		out = append(out, newParallelAction(decl))
	}
	return out, nil
}

func newParallelAction(decl Decl) action.AnyAction {
	members := append([]string(nil), decl.Members...)

	return action.New(decl.Name, func(ctx context.Context, input any) (any, error) {
		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal(fmt.Sprintf(
				"@parallel %q: no registry in context", decl.Name))
		}

		routes := make(map[string]action.AnyAction, len(members))
		for _, name := range members {
			target, ok := reg.Get(name)
			if !ok {
				return nil, xerr.NotFound(fmt.Sprintf(
					"@parallel %q: member %q is not in the registry",
					decl.Name, name))
			}
			routes[name] = target
		}

		return invokeParallel(ctx, routes, input)
	}).
		Description(fmt.Sprintf("Parallel group %q (%d members)",
			decl.Name, len(members))).
		Tag("flow", "parallel", "fanout").
		Build()
}

func invokeParallel(
	ctx context.Context,
	routes map[string]action.AnyAction,
	input any,
) (map[string]any, error) {
	results := make(map[string]any, len(routes))
	errs := make(map[string]error, len(routes))

	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, target := range routes {
		wg.Add(1)
		go func(n string, act action.AnyAction) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					errs[n] = xerr.PanicRecovery(r)
					mu.Unlock()
				}
			}()

			val, err := action.InvokeAny(ctx, act, input)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[n] = err
				return
			}
			results[n] = val
		}(name, target)
	}

	wg.Wait()

	if len(errs) > 0 {
		names := make([]string, 0, len(errs))
		for n := range errs {
			names = append(names, n)
		}
		sort.Strings(names)
		first := names[0]
		return results, fmt.Errorf(
			"@parallel: member %q failed: %w", first, errs[first])
	}
	return results, nil
}

func parseName(header string) (string, error) {
	rest, ok := core.StripDirectivePrefix(header, "parallel")
	if !ok {
		return "", fmt.Errorf("expected `@parallel NAME { ... }`, got %q", header)
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", fmt.Errorf("missing name — expected `@parallel NAME { ... }`")
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
			return nil, core.AtErrf(ctx, bodyLine, "parallel "+name,
				"expected one action name per line, got %q", line)
		}
		if seen[line] {
			return nil, core.AtErrf(ctx, bodyLine, "parallel "+name,
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
			return fmt.Errorf("invalid character %q in parallel name", string(c))
		}
	}
	return nil
}
