package match

import (
	"context"
	"strconv"

	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const defaultLabel = "default"

type Case struct {
	Condition string
	IsDefault bool
	Body      compiler.Expr
}

type Expr struct {
	Subject string
	Cases   []Case
}

func (*Expr) Node() {}

func (m *Expr) Children() []compiler.Expr {
	children := make([]compiler.Expr, len(m.Cases))
	for i := range m.Cases {
		children[i] = m.Cases[i].Body
	}
	return children
}

func (m *Expr) DescribeActionGraph() compiler.ActionGraphDescription {
	labels := make([]string, len(m.Cases))
	for i, current := range m.Cases {
		labels[i] = "case " + strconv.Itoa(i+1)
		if current.IsDefault {
			labels[i] = defaultLabel
		}
	}
	return compiler.ActionGraphDescription{
		Kind:        ID,
		Name:        "match.evaluate",
		Form:        compiler.ActionGraphChoice,
		ChildLabels: labels,
	}
}

func (m *Expr) Analyze(resolver compiler.CapabilityResolver) error {
	for i := range m.Cases {
		if err := m.Cases[i].Body.Analyze(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (m *Expr) Build(ctx context.Context, bCtx *compiler.BuildContext) (action.AnyAction, error) {
	type compiledCase struct {
		bodyAct action.AnyAction
	}

	compiledCases := make([]compiledCase, len(m.Cases))
	conditionCases := make([]ConditionCase, len(m.Cases))
	var children []action.IntrospectionChild
	if bCtx.Introspect {
		children = make([]action.IntrospectionChild, 0, len(m.Cases))
	}

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
		if bCtx.Introspect {
			role := "case"
			label := "case"
			if c.IsDefault {
				role, label = "default", "default"
			}
			children = append(children, action.IntrospectionChild{
				Node: act, Relation: compiler.IntrospectionKindBranch, Role: role, Label: label,
			})
		}
	}

	program := action.New("match.evaluate", func(execCtx context.Context, in any) (any, error) {
		env := WithSymbolEnvironment(execCtx, compiler.BuildEnv(in))

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
	}).Tag("control", "match").Build()
	composition := action.IntrospectionComposition{Kind: "match", Children: children}
	if bCtx.Introspect {
		composition.Opaque = true
		composition.Notes = []string{"case predicates and subject are closure-captured and omitted"}
	}
	return compiler.TrackActionComposition(bCtx, program, composition), nil
}
