package compiler

import (
	"maps"
	"reflect"

	"github.com/nexssp/kernel/xerr"
)

// TypeAnalysisContext carries the type information available at a point in a
// pipeline. Input is the current payload; Bindings contains outputs previously
// named with `as` in the same pipeline scope.
type TypeAnalysisContext struct {
	Resolver           CapabilityResolver
	Input              TypeDescriptor
	Bindings           map[string]TypeDescriptor
	SymbolNamespaces   map[string]SymbolNamespace
	CheckExpression    TypedSourceExpressionChecker
	ExpressionCompiler ExpressionCompiler
}

// TypedExpressionAnalyzer is an optional static-analysis contract for AST
// extensions that need the type of the current pipeline value or its `as`
// bindings. An analyzer is responsible for analyzing any child expressions
// whose context it owns. Unknown types are represented by a nil Type.
type TypedExpressionAnalyzer interface {
	AnalyzeTypes(TypeAnalysisContext) (TypeDescriptor, error)
}

// TypedExpressionCheck lets a core AST node delegate type-aware validation to
// the extension that parsed it, without making the compiler depend on that
// extension's expression language.
type TypedExpressionCheck func(TypeAnalysisContext) (TypeDescriptor, error)

// TypedSourceExpressionChecker compiles a source-language expression against
// the static input and binding types available at that point in the AST.
type TypedSourceExpressionChecker func(string, TypeAnalysisContext, Position) (TypeDescriptor, error)

// analyzeTypedExpressions propagates declared action output types through
// linear pipelines and lets typed AST extensions validate expressions against
// those types. Unknown or dynamic payloads remain unchecked rather than being
// rejected speculatively.
func analyzeTypedExpressions(ctx TypeAnalysisContext, root Expr) error {
	_, err := analyzeTypedNode(ctx, root)
	return err
}

// AnalyzeTypedExpression analyzes one AST node with the supplied static
// context. Composite extension nodes use this to analyze their children while
// preserving or intentionally narrowing the child's input context.
func AnalyzeTypedExpression(ctx TypeAnalysisContext, expr Expr) (TypeDescriptor, error) {
	if ctx.Resolver == nil {
		return TypeDescriptor{}, xerr.Internal("typed analysis: capability resolver is nil")
	}
	ctx.Bindings = cloneTypeBindings(ctx.Bindings)
	return analyzeTypedNode(ctx, expr)
}

func analyzeTypedNode(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, error) {
	if node == nil {
		return TypeDescriptor{}, nil
	}
	if output, handled, err := analyzeTypedPipelineNode(ctx, node); handled || err != nil {
		return output, err
	}
	if analyzer, ok := node.(TypedExpressionAnalyzer); ok {
		ctx.Bindings = cloneTypeBindings(ctx.Bindings)
		return analyzer.AnalyzeTypes(ctx)
	}
	if contract, ok := contractForExpr(ctx.Resolver, node); ok {
		return contract.Output, nil
	}
	return analyzeTypedExpressionNode(ctx, node)
}

func analyzeTypedPipelineNode(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, bool, error) {
	switch current := node.(type) {
	case *PipeExpr:
		return analyzeTypedPipe(ctx, current)
	case *BindingExpr:
		output, err := analyzeTypedNode(ctx, current.Inner)
		if err == nil {
			// The caller owns a cloned binding map for this analysis scope.
			ctx.Bindings[current.Name] = output
		}
		return output, true, err
	case *DecoratedExpr:
		output, err := analyzeTypedNode(ctx, current.Inner)
		return output, true, err
	default:
		return TypeDescriptor{}, false, nil
	}
}

func analyzeTypedPipe(ctx TypeAnalysisContext, pipe *PipeExpr) (TypeDescriptor, bool, error) {
	state := ctx.Input
	localBindings := cloneTypeBindings(ctx.Bindings)
	for _, step := range flattenPipe(pipe) {
		inner, binding := unwrapBinding(step)
		stepContext := ctx
		stepContext.Input = state
		stepContext.Bindings = localBindings
		output, err := analyzeTypedNode(stepContext, inner)
		if err != nil {
			return TypeDescriptor{}, true, err
		}
		if binding != nil {
			localBindings[binding.Name] = output
		}
		state = output
	}
	return state, true, nil
}

func analyzeTypedExpressionNode(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, error) {
	switch current := node.(type) {
	case *LiteralExpr:
		if current.Val != nil {
			return TypeDescriptor{Type: reflect.TypeOf(current.Val)}, nil
		}
	case *ProjectionExpr:
		return analyzeTypedProjection(ctx, current)
	case *ParallelExpr:
		return analyzeTypedParallel(ctx, current)
	case *FallbackExpr:
		return analyzeTypedFallback(ctx, current)
	case *ConditionalExpr:
		return analyzeTypedConditional(ctx, current)
	case *LoopExpr:
		return analyzeTypedLoop(ctx, current)
	case *AssertExpr:
		return analyzeTypedAssert(ctx, current)
	default:
		return analyzeTypedUnknownChildren(ctx, node)
	}
	return TypeDescriptor{}, nil
}

func analyzeTypedProjection(ctx TypeAnalysisContext, projection *ProjectionExpr) (TypeDescriptor, error) {
	if projection.TypeCheck == nil {
		return TypeDescriptor{}, nil
	}
	return projection.TypeCheck(ctx)
}

func analyzeTypedParallel(ctx TypeAnalysisContext, parallel *ParallelExpr) (TypeDescriptor, error) {
	for _, branch := range parallel.Branches {
		if _, err := analyzeTypedNode(ctx, branch); err != nil {
			return TypeDescriptor{}, err
		}
	}
	return TypeDescriptor{Type: reflect.TypeFor[map[string]any]()}, nil
}

func analyzeTypedFallback(ctx TypeAnalysisContext, fallback *FallbackExpr) (TypeDescriptor, error) {
	left, err := analyzeTypedNode(ctx, fallback.L)
	if err != nil {
		return TypeDescriptor{}, err
	}
	right, err := analyzeTypedNode(ctx, fallback.R)
	if err != nil {
		return TypeDescriptor{}, err
	}
	if left == right {
		return left, nil
	}
	return TypeDescriptor{}, nil
}

func analyzeTypedConditional(ctx TypeAnalysisContext, conditional *ConditionalExpr) (TypeDescriptor, error) {
	condition, err := analyzeTypedNode(ctx, conditional.Cond)
	if err != nil {
		return TypeDescriptor{}, err
	}
	if condition.Type != nil && condition.Type.Kind() != reflect.Bool && condition.Type.Kind() != reflect.Interface {
		return TypeDescriptor{}, xerr.Validation("invalid conditional condition", SourceError(conditional.Pos, "conditional: condition must be bool, got %s", condition.Type))
	}
	thenType, err := analyzeTypedNode(ctx, conditional.Then)
	if err != nil {
		return TypeDescriptor{}, err
	}
	if conditional.Else == nil {
		return TypeDescriptor{}, nil
	}
	elseType, err := analyzeTypedNode(ctx, conditional.Else)
	if err != nil {
		return TypeDescriptor{}, err
	}
	if thenType == elseType {
		return thenType, nil
	}
	return TypeDescriptor{}, nil
}

func analyzeTypedLoop(ctx TypeAnalysisContext, loop *LoopExpr) (TypeDescriptor, error) {
	output, err := analyzeTypedNode(ctx, loop.Body)
	if err != nil {
		return TypeDescriptor{}, err
	}
	if ctx.CheckExpression != nil {
		conditionContext := ctx
		conditionContext.Input = output
		condition, err := ctx.CheckExpression(loop.Until, conditionContext, loop.Pos)
		if err != nil {
			return TypeDescriptor{}, err
		}
		if condition.Type != nil && condition.Type.Kind() != reflect.Bool && condition.Type.Kind() != reflect.Interface {
			return TypeDescriptor{}, xerr.Validation("invalid loop condition", SourceError(loop.Pos, "loop: until condition must be bool, got %s", condition.Type))
		}
	}
	return output, nil
}

func analyzeTypedAssert(ctx TypeAnalysisContext, assertion *AssertExpr) (TypeDescriptor, error) {
	if ctx.CheckExpression != nil {
		condition, err := ctx.CheckExpression(assertion.Condition, ctx, assertion.Pos)
		if err != nil {
			return TypeDescriptor{}, err
		}
		if condition.Type != nil && condition.Type.Kind() != reflect.Bool && condition.Type.Kind() != reflect.Interface {
			return TypeDescriptor{}, xerr.Validation("invalid assertion condition", SourceError(assertion.Pos, "assert: condition must be bool, got %s", condition.Type))
		}
	}
	return ctx.Input, nil
}

func analyzeTypedUnknownChildren(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, error) {
	// Unknown extension composites may still contain typed expressions. Their
	// child input semantics are extension-specific, so retain exact `as`
	// bindings but do not guess each child's current payload type.
	for _, child := range node.Children() {
		childContext := ctx
		childContext.Input = TypeDescriptor{}
		if _, err := analyzeTypedNode(childContext, child); err != nil {
			return TypeDescriptor{}, err
		}
	}
	return TypeDescriptor{}, nil
}

func cloneTypeBindings(bindings map[string]TypeDescriptor) map[string]TypeDescriptor {
	if len(bindings) == 0 {
		return make(map[string]TypeDescriptor)
	}
	return maps.Clone(bindings)
}
