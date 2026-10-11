package compiler

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// BuildContext carries every dependency a node needs to lower itself
// into a runnable action. It is deliberately a small value struct: the
// AST knows how to Build itself, core just provides the pieces.
type BuildContext struct {
	Resolver   CapabilityResolver
	Modifiers  *ModifierTable
	Advisers   []AtomAdviseFunc
	Introspect bool
}

// Build lowers an AST into runnable action composition, resolving every
// capability through the supplied resolver. The nested composition is an
// implementation detail; BuildActionGraph exposes the logical data and
// control flow when a reviewable graph is requested.
//
// The function is a thin dispatcher: every node implements its own
// Build method. Core never switches on concrete AST types.
func Build(
	ctx context.Context,
	resolver CapabilityResolver,
	modifiers *ModifierTable,
	e Expr,
	advisers ...AtomAdviseFunc,
) (action.AnyAction, error) {
	return buildAction(ctx, resolver, modifiers, e, false, advisers...)
}

func buildAction(
	ctx context.Context,
	resolver CapabilityResolver,
	modifiers *ModifierTable,
	e Expr,
	introspect bool,
	advisers ...AtomAdviseFunc,
) (action.AnyAction, error) {
	bCtx := &BuildContext{
		Resolver:   resolver,
		Modifiers:  modifiers,
		Advisers:   advisers,
		Introspect: introspect,
	}
	return e.Build(ctx, bCtx)
}

func inspectBuiltAction(ctx *BuildContext, act action.AnyAction) action.AnyAction {
	if ctx != nil && ctx.Introspect {
		if _, ok := act.(action.IntrospectionProvider); ok {
			return act
		}
		if _, ok := act.(action.Composite); ok {
			return act
		}
	}
	return inspectBuiltComposition(ctx, act, action.IntrospectionComposition{Kind: IntrospectionKindAction, Opaque: true})
}

func inspectBuiltPair(
	ctx *BuildContext,
	act action.AnyAction,
	kind string,
	first, second action.IntrospectionChild,
) action.AnyAction {
	if ctx == nil || !ctx.Introspect {
		return act
	}
	return action.WithComposition(act, action.IntrospectionComposition{
		Kind: kind, Children: []action.IntrospectionChild{first, second},
	})
}

func inspectBuiltChild(
	ctx *BuildContext,
	act action.AnyAction,
	kind string,
	child action.IntrospectionChild,
) action.AnyAction {
	if ctx == nil || !ctx.Introspect {
		return act
	}
	return action.WithComposition(act, action.IntrospectionComposition{
		Kind: kind, Opaque: true, Children: []action.IntrospectionChild{child},
	})
}

func inspectBuiltComposition(
	ctx *BuildContext,
	act action.AnyAction,
	composition action.IntrospectionComposition,
) action.AnyAction {
	if ctx != nil && ctx.Introspect {
		return action.WithComposition(act, composition)
	}
	return act
}

// TrackActionComposition records explicit composition only when the current
// compilation opted into action introspection. Children and notes must be
// structural and must not contain argument or runtime values.
func TrackActionComposition(
	ctx *BuildContext,
	act action.AnyAction,
	composition action.IntrospectionComposition,
) action.AnyAction {
	return inspectBuiltComposition(ctx, act, composition)
}

// ComposedPipe is the Flow adapter for Kernel's dynamic pipe composition.
// Flow owns the DSL/lowering decision; Kernel owns invocation, cancellation,
// error propagation, and lifecycle behavior.
func ComposedPipe(left, right action.AnyAction) action.AnyAction {
	return action.PipeAny("pipe", left, right).Build()
}
