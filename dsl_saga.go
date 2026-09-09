package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// CompileSaga parses Arrow DSL with embedded transaction rollbacks into a Saga Node.
func CompileSaga(expr string, reg Registry) (*action.Builder[any, any], error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, xerr.BadRequest("flow: saga expression cannot be empty")
	}

	parser := compiler.NewParser(expr)
	ast, err := parser.ParseExpression()
	if err != nil {
		return nil, err
	}

	return compileSagaAST(ast, reg)
}

func compileSagaAST(node compiler.Expr, reg Registry) (*action.Builder[any, any], error) {
	switch n := node.(type) {

	case *compiler.ParallelExpr:
		routes := make(map[string]action.AnyAction, len(n.Children))
		for i, child := range n.Children {
			bld, err := compileSagaAST(child, reg)
			if err != nil {
				return nil, err
			}
			routes[fmt.Sprintf("saga_branch_%d", i+1)] = bld.Build()
		}

		parallel := action.ParallelNamed[any]("parallel_sagas", routes)
		return action.New("parallel_sagas_wrap", func(ctx context.Context, req any) (any, error) {
			return parallel.Build().Do(ctx, req)
		}), nil

	case *compiler.PipelineExpr:
		// Collect atoms into a sequence of Saga steps
		atoms, err := flattenPipeline(n)
		if err != nil {
			return nil, err
		}

		steps := make([]nodes.SagaStep, 0, len(atoms))
		for _, atom := range atoms {
			forwardAct, ok := reg.Get(atom.Name)
			if !ok {
				return nil, fmt.Errorf("flow: saga capability %q not found", atom.Name)
			}

			var compensateAct action.AnyAction
			if rollbackName, ok := atom.Params["rollback"]; ok {
				compensateAct, _ = reg.Get(rollbackName)
			} else if rollbackInput, ok := atom.Inputs["rollback"]; ok {
				compensateAct, _ = reg.Get(rollbackInput)
			}

			steps = append(steps, nodes.SagaStep{
				NodeID:     atom.Name,
				Forward:    forwardAct,
				Compensate: compensateAct,
			})
		}
		return nodes.NewDynamicSaga("saga_pipe", steps), nil

	case *compiler.AtomExpr:
		// Single atom saga
		forwardAct, ok := reg.Get(n.Name)
		if !ok {
			return nil, fmt.Errorf("flow: saga capability %q not found", n.Name)
		}
		var compensateAct action.AnyAction
		if rollbackName, ok := n.Params["rollback"]; ok {
			compensateAct, _ = reg.Get(rollbackName)
		} else if rollbackInput, ok := n.Inputs["rollback"]; ok {
			compensateAct, _ = reg.Get(rollbackInput)
		}
		step := nodes.SagaStep{
			NodeID:     n.Name,
			Forward:    forwardAct,
			Compensate: compensateAct,
		}
		return nodes.NewDynamicSaga("saga_single", []nodes.SagaStep{step}), nil

	default:
		// If it's not a pipe, parallel, or atom, try to compile it normally and wrap it
		return compileAST(node, reg)
	}
}

// flattenPipeline unrolls a deeply nested PipelineExpr tree into a flat list of Atoms
func flattenPipeline(node compiler.Expr) ([]*compiler.AtomExpr, error) {
	switch n := node.(type) {
	case *compiler.PipelineExpr:
		left, err := flattenPipeline(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := flattenPipeline(n.Right)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	case *compiler.AtomExpr:
		return []*compiler.AtomExpr{n}, nil
	default:
		return nil, fmt.Errorf("flow: nested node %T inside saga pipeline must be an atom", node)
	}
}
