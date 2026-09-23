package at_route

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

var routeNodeCounter atomic.Int64

// RouteWhenSpec is the runtime form of one `when COND -> TARGET` clause.
type RouteWhenSpec struct {
	Condition string
	Target    string
}

// RouteSpec is the runtime form of one `@route NAME { ... }` block.
type RouteSpec struct {
	Name  string
	Whens []RouteWhenSpec
	Else  string
}

// NewRouteAction compiles conditions once and returns an action that
// evaluates them against the input, then invokes the first matched
// target. Targets are resolved from the registry installed on the
// execution context.
func NewRouteAction(spec RouteSpec) (*action.BuiltAction[any, any], error) {
	if spec.Name == "" {
		spec.Name = fmt.Sprintf("route_%d", routeNodeCounter.Add(1))
	}
	if len(spec.Whens) == 0 {
		return nil, fmt.Errorf("flow: @route %q has no when clauses", spec.Name)
	}

	programs := make([]*vm.Program, len(spec.Whens))
	for i, w := range spec.Whens {
		if w.Target == "" {
			return nil, fmt.Errorf("flow: @route %q when[%d]: empty target", spec.Name, i)
		}
		pre, _ := nodes.PreprocessDotNotation(w.Condition)
		prog, err := expr.Compile(pre, expr.AllowUndefinedVariables())
		if err != nil {
			return nil, fmt.Errorf(
				"flow: @route %q when[%d] invalid condition %q: %w",
				spec.Name, i, w.Condition, err,
			)
		}
		programs[i] = prog
	}

	return action.New(spec.Name, func(ctx context.Context, input any) (any, error) {
		reg := contracts.RegistryFromContext(ctx)
		if reg == nil {
			return nil, xerr.Internal(fmt.Sprintf(
				"@route %q: no registry in context; use flow/runner or set "+
					"contracts.WithRegistry before invoking", spec.Name))
		}

		env := nodes.NormalizeEnv(input)

		for i, w := range spec.Whens {
			res, runErr := expr.Run(programs[i], env)
			if runErr != nil {
				return nil, xerr.Internal(fmt.Sprintf(
					"@route %q: when[%d] %q evaluation failed",
					spec.Name, i, w.Condition), runErr)
			}
			matched, _ := res.(bool)
			if !matched {
				continue
			}
			target, ok := reg.Get(w.Target)
			if !ok {
				return nil, xerr.NotFound(fmt.Sprintf(
					"@route %q: matched when[%d] but target %q is not in the registry",
					spec.Name, i, w.Target))
			}
			return action.InvokeAny(ctx, target, input)
		}

		if spec.Else != "" {
			target, ok := reg.Get(spec.Else)
			if !ok {
				return nil, xerr.NotFound(fmt.Sprintf(
					"@route %q: no when-clause matched and else target %q is not in the registry",
					spec.Name, spec.Else))
			}
			return action.InvokeAny(ctx, target, input)
		}

		return nil, xerr.Validation(fmt.Sprintf(
			"@route %q: no when-clause matched and no else branch is defined",
			spec.Name))
	}).
		Description(fmt.Sprintf("Route %q (%d when-clauses, else=%v)",
			spec.Name, len(spec.Whens), spec.Else != "")).
		Tag("flow", "route", "branch").
		Build(), nil
}
