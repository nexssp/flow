package core

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// BuildContext carries every dependency a node needs to lower itself
// into a runnable action. It is deliberately a small value struct: the
// AST knows how to Build itself, core just provides the pieces.
type BuildContext struct {
	Resolver  CapabilityResolver
	Modifiers *ModifierTable
	Advisers  []AtomAdviseFunc
}

// Build lowers an AST into a runnable action tree, resolving every
// capability through the supplied resolver.
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
	bCtx := &BuildContext{
		Resolver:  resolver,
		Modifiers: modifiers,
		Advisers:  advisers,
	}
	return e.Build(ctx, bCtx)
}

// ComposedPipe is the Flow adapter for Kernel's dynamic pipe composition.
// Flow owns the DSL/lowering decision; Kernel owns invocation, cancellation,
// error propagation, and lifecycle behavior.
func ComposedPipe(left, right action.AnyAction) action.AnyAction {
	return action.PipeAny("pipe", left, right).Build()
}
