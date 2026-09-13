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

// CompilePipeline parses the Arrow DSL into an AST and compiles it into an executable Builder.
func CompilePipeline(expr string, reg Registry) (*action.Builder[any, any], error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, xerr.BadRequest("flow: pipeline expression cannot be empty")
	}

	parser := compiler.NewParser(expr)

	ast, err := parser.ParseExpression()
	if err != nil {
		return nil, err
	}

	return compileAST(ast, reg)
}

func compileAST(node compiler.Expr, reg Registry) (*action.Builder[any, any], error) {
	switch n := node.(type) {
	case *compiler.PipelineExpr:
		left, err := compileAST(n.Left, reg)
		if err != nil {
			return nil, err
		}

		right, err := compileAST(n.Right, reg)
		if err != nil {
			return nil, err
		}

		builtRight := right.Build()
		pipeBld := action.Pipe[any, any, any]("pipe", left.Build(), builtRight)

		// Hoist routes, status, description, and name from the terminal node to the composite pipeline
		for _, b := range builtRight.GetBindings() {
			pipeBld.Route(b)
		}

		if meta := builtRight.Describe(); meta != nil {
			if meta.Name != "" && meta.Name != "pipe" {
				pipeBld.Name(meta.Name)
			}

			if meta.Description != "" {
				pipeBld.Description(meta.Description)
			}

			if meta.SuccessStatus > 0 {
				pipeBld.SuccessStatus(meta.SuccessStatus)
			}
		}

		return pipeBld, nil

	case *compiler.ParallelExpr:
		routes := make(map[string]action.AnyAction, len(n.Children))
		for i, child := range n.Children {
			childBld, err := compileAST(child, reg)
			if err != nil {
				return nil, err
			}

			built := childBld.Build()

			name := built.Describe().Name
			if name == "" || strings.HasPrefix(name, "unknown") {
				name = fmt.Sprintf("branch_%d", i+1)
			}

			routes[name] = built
		}

		parallel := action.ParallelNamed[any]("parallel_group", routes)

		return action.New("parallel_wrap", func(ctx context.Context, req any) (any, error) {
			return parallel.Build().Do(ctx, req)
		}), nil

	case *compiler.FallbackExpr:
		left, err := compileAST(n.Left, reg)
		if err != nil {
			return nil, err
		}

		right, err := compileAST(n.Right, reg)
		if err != nil {
			return nil, err
		}

		name := left.Build().Describe().Name
		if name == "" {
			name = "fallback_chain"
		}

		return action.FirstSuccess(name, left, right), nil

	case *compiler.ConditionalExpr:
		gateBld, err := compileAST(n.Gate, reg)
		if err != nil {
			return nil, err
		}

		targetBld, err := compileAST(n.Target, reg)
		if err != nil {
			return nil, err
		}

		routes := map[string]action.AnyAction{
			"proceed": targetBld.Build(),
			"skip": action.New("skip", func(_ context.Context, input any) (any, error) {
				return input, nil
			}).Build(),
		}

		branch := action.BranchAny("conditional_gate", routes, func(_ context.Context, input any) (string, error) {
			if isTruthy(input) {
				return "proceed", nil
			}

			return "skip", nil
		})

		return action.Pipe[any, any, any]("gate_pipe", gateBld.Build(), branch.Build()), nil

	case *compiler.ProjectionExpr:
		projAct, err := nodes.NewProjectionAction(n.Raw)
		if err != nil {
			return nil, err
		}

		return action.Dynamic(projAct), nil

	case *compiler.LoopExpr:
		bodyBld, err := compileAST(n.Body, reg)
		if err != nil {
			return nil, err
		}

		loopAct, err := nodes.NewLoopAction(bodyBld.Build(), n.Until, 15)
		if err != nil {
			return nil, err
		}

		return action.Dynamic(loopAct), nil

	case *compiler.AtomExpr:
		return resolveDynamicNode(n, reg)

	default:
		return nil, fmt.Errorf("flow: unknown AST node type %T", node)
	}
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}

	switch val := v.(type) {
	case bool:
		return val
	case int, int64:
		return val != 0
	case string:
		lower := strings.ToLower(val)

		return lower == "true" || lower == "ok" || lower == "approved"
	case map[string]any:
		if approved, ok := val["approved"].(bool); ok {
			return approved
		}
	}

	return true
}
