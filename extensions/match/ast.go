package match

import (
	"context"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

type Case struct {
	Condition string
	IsDefault bool
	Body      core.Expr
}

type Expr struct {
	Subject string
	Cases   []Case
}

func (*Expr) Node() {}

func (m *Expr) Children() []core.Expr {
	children := make([]core.Expr, len(m.Cases))
	for i := range m.Cases {
		children[i] = m.Cases[i].Body
	}
	return children
}

func (m *Expr) Analyze(resolver core.CapabilityResolver) error {
	for i := range m.Cases {
		if err := m.Cases[i].Body.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (m *Expr) Build(ctx context.Context, bCtx *core.BuildContext) (action.AnyAction, error) {
	type compiledCase struct {
		isDefault bool
		condProg  *vm.Program
		bodyAct   action.AnyAction
	}

	compiledCases := make([]compiledCase, len(m.Cases))

	var subjectProg *vm.Program
	if m.Subject != "" {
		cleaned := core.PreprocessDots(m.Subject)
		prog, err := expr.Compile(cleaned, expr.AllowUndefinedVariables())
		if err != nil {
			return nil, xerr.Validation("match: invalid subject expression: "+m.Subject, err)
		}
		subjectProg = prog
	}

	for i, c := range m.Cases {
		act, err := c.Body.Build(ctx, bCtx)
		if err != nil {
			return nil, err
		}

		cc := compiledCase{
			isDefault: c.IsDefault,
			bodyAct:   act,
		}

		if !c.IsDefault {
			condText := c.Condition
			if m.Subject != "" {
				condText = "__match_subject__ == (" + condText + ")"
			} else {
				condText = core.PreprocessDots(condText)
			}

			prog, err := expr.Compile(condText, expr.AllowUndefinedVariables())
			if err != nil {
				return nil, xerr.Validation("match: invalid case condition: "+c.Condition, err)
			}
			cc.condProg = prog
		}

		compiledCases[i] = cc
	}

	return action.New("match", func(execCtx context.Context, in any) (any, error) {
		env := core.BuildEnv(in)

		if subjectProg != nil {
			subjVal, err := expr.Run(subjectProg, env)
			if err != nil {
				return nil, xerr.BadRequest("match: failed to evaluate subject: "+m.Subject, err)
			}
			env["__match_subject__"] = subjVal
		}

		for _, cc := range compiledCases {
			if cc.isDefault {
				return action.InvokeAny(execCtx, cc.bodyAct, in)
			}

			matched, err := expr.Run(cc.condProg, env)
			if err != nil {
				continue
			}

			if isTruthy(matched) {
				return action.InvokeAny(execCtx, cc.bodyAct, in)
			}
		}

		return in, nil
	}).Tag("control", "match").Build(), nil
}

func isTruthy(val any) bool {
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}
