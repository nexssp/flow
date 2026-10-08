package on_error

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/extensions/match"
)

func wrapFromMeta(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	cfg, _ := meta["on_error"].(Config)
	if len(cfg.Rules) == 0 && cfg.ElseTarget == "" {
		return inner, nil
	}
	return wrap("on_error", inner, cfg), nil
}

func wrap(name string, protected action.AnyAction, cfg Config) action.AnyAction {
	conditions := compileRules(cfg)
	return action.CatchAny(name, protected, func(ctx context.Context, req any, err error) (any, error) {
		return recoverFn(ctx, req, err, cfg, conditions)
	}).Build()
}

func recoverFn(
	ctx context.Context,
	req any,
	err error,
	cfg Config,
	conditions []match.ConditionCase,
) (any, error) {
	if terminalErr := terminalRecoveryError(ctx, err); terminalErr != nil {
		return nil, terminalErr
	}
	errKind := string(xerr.From(err).Kind)
	env := pipelineErrorEnvironment(req, err)

	target := resolveTargetWithConditions(cfg, env, conditions)
	if target == "" {
		return nil, err
	}

	resolver := contracts.ActionResolverFromContext(ctx)
	if resolver == nil {
		return nil, xerr.Internal("on_error: no action resolver in context, cannot resolve " + target)
	}
	targetAction, ok := resolver.Action(target)
	if !ok {
		return nil, xerr.NotFound("on_error: target action " + target + " not found in registry")
	}

	recErr := &contracts.RecoveredError{Err: err, Kind: errKind}
	recoveryCtx := contracts.WithRecoveredError(ctx, recErr)
	return action.InvokeAny(recoveryCtx, targetAction, req)
}

func resolveTarget(cfg Config, env map[string]any) string {
	return resolveTargetWithConditions(cfg, env, compileRules(cfg))
}

func compileRules(cfg Config) []match.ConditionCase {
	conditions := make([]match.ConditionCase, len(cfg.Rules))
	for i, rule := range cfg.Rules {
		program, err := match.CompileCondition(rule.Condition)
		if err == nil {
			conditions[i].Program = program
		}
	}
	return conditions
}

func resolveTargetWithConditions(cfg Config, env map[string]any, conditions []match.ConditionCase) string {
	// on_error is deliberately tolerant: a condition that cannot be
	// evaluated against the current payload is treated as no-match, and
	// routing falls through to the next rule or to the else target.
	index := match.FirstMatchingCase(conditions, env)
	if index >= 0 && index < len(cfg.Rules) {
		return cfg.Rules[index].Target
	}
	return cfg.ElseTarget
}
