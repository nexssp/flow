package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xerr"
)

// RecoveryMatch selects which error property a rule tests.
type RecoveryMatch uint8

const (
	// MatchByKind compares xerr.KindFrom(err) to Rule.Kind.
	MatchByKind RecoveryMatch = iota + 1

	// MatchBySuspended matches errors.Is(err, dag.ErrSuspended).
	MatchBySuspended
)

// RecoveryRule is one `when COND -> TARGET` clause in runtime form.
type RecoveryRule struct {
	Match  RecoveryMatch
	Kind   string
	Target string
}

// RecoverySpec is the runtime form of a recovery block.
type RecoverySpec struct {
	Rules []RecoveryRule
	Else  string
}

// ParseRecoveryBlock parses the body lines of a recovery block — the
// lines between `{` and `}`. It returns the rules and the else target.
//
// baseLine is the index of the block header so error positions point
// at the file the block lives in. directiveName is used in error
// messages ("on_error", "fallback").
//
// Shared by @on_error and @fallback because the block grammar is
// identical for both.
func ParseRecoveryBlock(body []string, ctx *Context, baseLine int, directiveName string) ([]RecoveryRule, string, error) {
	var (
		rules []RecoveryRule
		els   string
	)
	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" ||
			strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "//") {
			continue
		}
		bodyLine := baseLine + offset + 2

		switch {
		case strings.HasPrefix(line, "when "):
			rule, err := parseRecoveryWhen(line, ctx, bodyLine, directiveName)
			if err != nil {
				return nil, "", err
			}
			rules = append(rules, rule)

		case strings.HasPrefix(line, "else"):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "else"))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "->"))
			if rest == "" {
				return nil, "", AtErrf(ctx, bodyLine, directiveName,
					"empty else target in %q", line)
			}
			if els != "" {
				return nil, "", AtErrf(ctx, bodyLine, directiveName,
					"duplicate else clause (already set to %q)", els)
			}
			els = rest

		default:
			return nil, "", AtErrf(ctx, bodyLine, directiveName,
				"unexpected line %q (expected `when ...` or `else -> ...`)", line)
		}
	}
	return rules, els, nil
}

// NewRecoveryAction wraps a whole pipeline with rule-based routing.
// Used by @on_error.
func NewRecoveryAction(
	name string,
	upstream action.Executable,
	spec RecoverySpec,
) (*action.BuiltAction[any, any], error) {
	if name == "" {
		name = "recovery"
	}
	if upstream == nil {
		return nil, fmt.Errorf("@%s: upstream action is nil", name)
	}
	if len(spec.Rules) == 0 && spec.Else == "" {
		return nil, fmt.Errorf("@%s: no rules and no else", name)
	}

	return action.New(name, func(ctx context.Context, input any) (any, error) {
		out, err := upstream.ExecuteDecoded(ctx, func(target any) error {
			return assignPayload(input, target)
		})
		if err != nil {
			return DispatchRecovery(ctx, input, err, spec)
		}
		return out, nil
	}).
		Description(fmt.Sprintf("Recovery router (%d rules, else=%v)",
			len(spec.Rules), spec.Else != "")).
		Tag("flow", "recovery", "error").
		Build(), nil
}

// RecoveryMiddleware wraps a single atom with rule-based routing. Used
// by @fallback. Install with UseFirst so the middleware is outermost
// and observes timeout, retry, and cancellation failures from the
// whole inner chain.
func RecoveryMiddleware(spec RecoverySpec) action.Middleware[any, any] {
	return func(next action.Fn[any, any]) action.Fn[any, any] {
		return func(ctx context.Context, req any) (any, error) {
			out, err := next(ctx, req)
			if err != nil {
				return DispatchRecovery(ctx, req, err, spec)
			}
			return out, nil
		}
	}
}

// DispatchRecovery routes a failure according to spec. On success
// (err == nil) it returns input unchanged. On failure the first
// matching rule wins; else is the fallback; if nothing matches the
// original error is returned unchanged.
func DispatchRecovery(ctx context.Context, input any, err error, spec RecoverySpec) (any, error) {
	if err == nil {
		return input, nil
	}
	reg := contracts.RegistryFromContext(ctx)
	if reg == nil {
		return nil, err
	}

	kind := string(xerr.KindFrom(err))
	suspended := errors.Is(err, dag.ErrSuspended)

	recovered := &contracts.RecoveredError{
		Err:       err,
		Kind:      kind,
		Suspended: suspended,
	}
	recoveryCtx := contracts.WithRecoveredError(ctx, recovered)

	for _, rule := range spec.Rules {
		if !recoveryRuleMatches(rule, kind, suspended) {
			continue
		}
		target, ok := reg.Get(rule.Target)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"recovery: matched rule target %q is not in the registry",
				rule.Target))
		}
		return action.InvokeAny(recoveryCtx, target, input)
	}

	if spec.Else != "" {
		target, ok := reg.Get(spec.Else)
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"recovery: else target %q is not in the registry",
				spec.Else))
		}
		return action.InvokeAny(recoveryCtx, target, input)
	}

	return nil, err
}

func recoveryRuleMatches(rule RecoveryRule, kind string, suspended bool) bool {
	switch rule.Match {
	case MatchBySuspended:
		return suspended
	case MatchByKind:
		return rule.Kind != "" && rule.Kind == kind
	}
	return false
}

func parseRecoveryWhen(line string, ctx *Context, bodyLine int, directiveName string) (RecoveryRule, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "when "))

	arrow := strings.Index(rest, "->")
	if arrow < 0 {
		return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
			"expected `when COND -> TARGET`, got %q", line)
	}

	cond := strings.TrimSpace(rest[:arrow])
	target := strings.TrimSpace(rest[arrow+2:])
	if cond == "" || target == "" {
		return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
			"empty condition or target in %q", line)
	}

	rule := RecoveryRule{Target: target}

	switch {
	case cond == "error.suspended":
		rule.Match = MatchBySuspended

	case strings.HasPrefix(cond, "error.kind"):
		eq := strings.Index(cond, "==")
		if eq < 0 {
			return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
				"expected `error.kind == \"Kind\"`, got %q", cond)
		}
		raw := strings.TrimSpace(cond[eq+2:])
		raw = strings.Trim(raw, `"'`)
		if raw == "" {
			return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
				"empty kind in %q", cond)
		}
		if !IsKnownXerrKind(raw) {
			return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
				"unknown xerr kind %q (known: %s)",
				raw, strings.Join(KnownXerrKinds(), ", "))
		}
		rule.Match = MatchByKind
		rule.Kind = raw

	default:
		return RecoveryRule{}, AtErrf(ctx, bodyLine, directiveName,
			"unsupported condition %q; supported: `error.kind == \"Kind\"`, `error.suspended`", cond)
	}

	return rule, nil
}

func assignPayload(input any, target any) error {
	if target == nil || input == nil {
		return nil
	}
	if ptr, ok := target.(*any); ok {
		*ptr = input
		return nil
	}
	return action.Assign(target, input)
}
