package on_error

import (
	"context"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

func wrapFromMeta(meta map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	cfg, _ := meta["on_error"].(Config)
	if len(cfg.Rules) == 0 && cfg.ElseTarget == "" {
		return inner, nil
	}
	return wrap("on_error", inner, cfg), nil
}

func wrap(name string, protected action.AnyAction, cfg Config) action.AnyAction {
	return action.CatchAny(name, protected, func(ctx context.Context, req any, err error) (any, error) {
		return recoverFn(ctx, req, err, cfg)
	}).Build()
}

func recoverFn(ctx context.Context, req any, err error, cfg Config) (any, error) {
	errKind := string(xerr.KindFrom(err))

	env := map[string]any{
		"error": map[string]any{
			"kind":    errKind,
			"message": err.Error(),
		},
		"input": req,
	}

	target := resolveTarget(cfg, env)
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
	for _, rule := range cfg.Rules {
		program, err := expr.Compile(rule.Condition, expr.AllowUndefinedVariables())
		if err != nil {
			continue
		}
		output, err := expr.Run(program, env)
		if err != nil {
			continue
		}
		if matched, ok := output.(bool); ok && matched {
			return rule.Target
		}
	}
	return cfg.ElseTarget
}
