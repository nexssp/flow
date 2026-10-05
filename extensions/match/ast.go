package match

import (
	"context"

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
		bodyAct action.AnyAction
	}

	compiledCases := make([]compiledCase, len(m.Cases))
	conditionCases := make([]ConditionCase, len(m.Cases))

	var subjectProg *vm.Program
	if m.Subject != "" {
		prog, err := CompileCondition(m.Subject)
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
			bodyAct: act,
		}
		conditionCases[i].IsDefault = c.IsDefault

		if !c.IsDefault {
			condText := c.Condition
			if m.Subject != "" {
				condText = "__match_subject__ == (" + condText + ")"
			}

			prog, err := CompileCondition(condText)
			if err != nil {
				return nil, xerr.Validation("match: invalid case condition: "+c.Condition, err)
			}
			conditionCases[i].Program = prog
		}

		compiledCases[i] = cc
	}

	return action.New("match.evaluate", func(execCtx context.Context, in any) (any, error) {
		env := WithKindEnvironment(core.BuildEnv(in))

		if subjectProg != nil {
			subjVal, err := EvaluateProgram(subjectProg, env)
			if err != nil {
				return nil, xerr.BadRequest("match: failed to evaluate subject: "+m.Subject, err)
			}
			env["__match_subject__"] = subjVal
		}

		caseIndex := FirstMatchingCase(conditionCases, env)
		if caseIndex >= 0 {
			return action.InvokeAny(execCtx, compiledCases[caseIndex].bodyAct, in)
		}

		return in, nil
	}).Tag("control", "match").Build(), nil
}
