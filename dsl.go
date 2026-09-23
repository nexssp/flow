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

// CompilePipeline parses a compact arrow expression and returns a
// builder that runs it. Options are applied before the AST walk; see
// compile_options.go for what can be configured.
//
// If a flow registry is attached via WithFlowRegistry, the AST is
// pre-scanned for stream atoms. Pipelines containing any registered
// source or operator are compiled by the stream compiler; all others
// take the existing unary path.
func CompilePipeline(expr string, reg *action.Registry, opts ...CompileOption) (*action.Builder[any, any], error) {
	expr = SanitizeDSL(expr)
	if strings.TrimSpace(expr) == "" {
		return nil, xerr.BadRequest("flow: pipeline expression cannot be empty")
	}

	parser := compiler.NewParser(expr)

	ast, err := parser.ParseExpression()
	if err != nil {
		return nil, err
	}

	options := applyCompileOptions(opts)
	if options.flowRegistry == nil {
		if defaultRegistry := DefaultRegistry(); defaultRegistry != nil {
			options.flowRegistry = defaultRegistry
		} else if reg != nil {
			options.flowRegistry = RegistryFromActionRegistry(reg)
		}
	}

	switch detectPipelineMode(ast, options.flowRegistry) {
	case pipelineModeSegment:
		return compileSegmentedPipeline(ast, reg, options)
	case pipelineModeStream:
		return compileStreamPipeline(ast, options)
	default:
		return compileAST(ast, reg, options)
	}
}

func compileAST(node compiler.Expr, reg *action.Registry, opts *compileOptions) (*action.Builder[any, any], error) {
	switch n := node.(type) {

	case *compiler.PipelineExpr:
		left, err := compileAST(n.Left, reg, opts)
		if err != nil {
			return nil, err
		}
		right, err := compileAST(n.Right, reg, opts)
		if err != nil {
			return nil, err
		}

		builtRight := right.Build()
		pipeBld := action.Pipe[any, any, any]("pipe", left.Build(), builtRight)

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
			childBld, err := compileAST(child, reg, opts)
			if err != nil {
				return nil, err
			}
			built := childBld.Build()

			name := built.Describe().Name
			if name == "" || strings.HasPrefix(name, "unknown") {
				name = fmt.Sprintf("branch_%d", i+1)
			}

			// Each concurrent branch must receive an isolated copy of the input map to avoid data races.
			branchAct := action.New(name, func(ctx context.Context, in any) (any, error) {
				if inMap, ok := in.(map[string]any); ok {
					cloned := make(map[string]any, len(inMap))
					for k, v := range inMap {
						cloned[k] = v
					}
					return built.Do(ctx, cloned)
				}
				return built.Do(ctx, in)
			}).Build()

			routes[name] = branchAct
		}

		parallel := action.ParallelNamed[any]("parallel_group", routes)

		return action.New("parallel_wrap", func(ctx context.Context, req any) (any, error) {
			branchMap, err := parallel.Build().Do(ctx, req)
			if err != nil {
				return nil, err
			}

			// Preserve upstream pipeline fields alongside parallel branch outputs
			if reqMap, ok := req.(map[string]any); ok {
				merged := make(map[string]any, len(reqMap)+len(branchMap))
				for k, v := range reqMap {
					merged[k] = v
				}
				for k, v := range branchMap {
					merged[k] = v
				}
				return merged, nil
			}
			return branchMap, nil
		}), nil

	case *compiler.FallbackExpr:
		left, err := compileAST(n.Left, reg, opts)
		if err != nil {
			return nil, err
		}
		right, err := compileAST(n.Right, reg, opts)
		if err != nil {
			return nil, err
		}

		name := left.Build().Describe().Name
		if name == "" {
			name = "fallback_chain"
		}
		return action.FirstSuccess(name, left, right), nil

	case *compiler.ConditionalExpr:
		var gateBld *action.Builder[any, any]
		var gateAtom *compiler.AtomExpr

		// If gate is an atom that is not registered as an action, treat it as a passthrough gate.
		if atom, ok := n.Gate.(*compiler.AtomExpr); ok {
			gateAtom = atom
			if reg == nil || !actionRegistryHas(reg, atom.Name) {
				gateBld = action.New(atom.Name, func(_ context.Context, input any) (any, error) {
					return input, nil
				})
			}
		}

		if gateBld == nil {
			var err error
			gateBld, err = compileAST(n.Gate, reg, opts)
			if err != nil {
				return nil, err
			}
		}

		targetBld, err := compileAST(n.Target, reg, opts)
		if err != nil {
			return nil, err
		}

		routes := map[string]action.AnyAction{
			"proceed": targetBld.Build(),
		}

		if n.Else != nil {
			elseBld, err := compileAST(n.Else, reg, opts)
			if err != nil {
				return nil, err
			}
			routes["else"] = elseBld.Build()
		} else {
			routes["skip"] = action.New("skip", func(_ context.Context, input any) (any, error) {
				return input, nil
			}).Build()
		}

		branch := action.BranchAny("conditional_gate", routes, func(_ context.Context, input any) (string, error) {
			if isTruthy(input) {
				return "proceed", nil
			}
			if m, ok := input.(map[string]any); ok && gateAtom != nil {
				if b, ok := m[gateAtom.Name].(bool); ok && b {
					return "proceed", nil
				}
			}
			if _, hasElse := routes["else"]; hasElse {
				return "else", nil
			}
			return "skip", nil
		})

		return action.Pipe[any, any, any]("gate_pipe", gateBld.Build(), branch.Build()), nil

	case *compiler.AssertExpr:
		assertAction, err := nodes.NewAssertAction(n.Condition, n.Message)
		if err != nil {
			return nil, err
		}
		return action.Dynamic(assertAction), nil

	case *compiler.ProjectionExpr:
		projAct, err := nodes.NewProjectionAction(n.Raw)
		if err != nil {
			return nil, err
		}
		return action.Dynamic(projAct), nil

	case *compiler.LoopExpr:
		bodyBld, err := compileAST(n.Body, reg, opts)
		if err != nil {
			return nil, err
		}

		loopAct, err := nodes.NewLoopAction(bodyBld.Build(), n.Until, 15)
		if err != nil {
			return nil, err
		}
		return action.Dynamic(loopAct), nil

	case *compiler.AtomExpr:
		return resolveDynamicNode(n, reg, opts)

	default:
		return nil, fmt.Errorf("flow: unknown AST node type %T", node)
	}
}

func actionRegistryHas(registry *action.Registry, name string) bool {
	if registry == nil {
		return false
	}
	_, ok := registry.Get(name)
	return ok
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}

	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case string:
		lower := strings.ToLower(val)
		return lower != "" && lower != "false" && lower != "no" && lower != "0"
	case map[string]any:
		if approved, ok := val["approved"].(bool); ok {
			return approved
		}
		if passed, ok := val["passed"].(bool); ok {
			return passed
		}
	}
	return true
}
