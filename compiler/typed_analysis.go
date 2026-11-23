package compiler

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/secret"
)

// TypeAnalysisContext carries the type information available at a point in a
// pipeline. Input is the current payload; Bindings contains outputs previously
// named with `as` in the same pipeline scope.
type TypeAnalysisContext struct {
	Resolver CapabilityResolver
	Input    TypeDescriptor
	// OriginalInput is the payload supplied when the expression scope began.
	// A nil pointer preserves standalone-analysis behavior by treating Input as
	// the original request; a non-nil descriptor may intentionally have no type.
	OriginalInput    *TypeDescriptor
	Bindings         map[string]TypeDescriptor
	PipelineBindings map[string]struct{}
	// PipelineInputBindings identifies named inputs declared by a pipeline
	// signature; unlike `as` outputs, these names select the typed request root.
	PipelineInputBindings map[string]struct{}
	SymbolNamespaces      map[string]SymbolNamespace
	ArgSchemas            map[string][]ArgFieldSpec
	CheckExpression       TypedSourceExpressionChecker
	ExpressionCompiler    ExpressionCompiler
	projectionInput       bool
}

// TypedExpressionAnalyzer is an optional static-analysis contract for AST
// extensions that need the type of the current pipeline value or its `as`
// bindings. An analyzer is responsible for analyzing any child expressions
// whose context it owns. Unknown types are represented by a nil Type.
type TypedExpressionAnalyzer interface {
	AnalyzeTypes(TypeAnalysisContext) (TypeDescriptor, error)
}

// TypedProjectionOutput marks expressions whose inferred output is a Flow
// projection and therefore uses projection-specific edge compatibility.
type TypedProjectionOutput interface {
	Expr
	ProjectionOutput()
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
	ctx = withPipelineBindingNames(ctx)
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
	ctx = withPipelineBindingNames(ctx)
	ctx.Bindings = cloneTypeBindings(ctx.Bindings)
	return analyzeTypedNode(ctx, expr)
}

// AnalyzeTypedExpressionWithBindings analyzes one AST node and returns the
// lexical bindings visible at its end. Composite expressions use this when
// validating a handler that runs after the analyzed expression completes.
func AnalyzeTypedExpressionWithBindings(
	ctx TypeAnalysisContext,
	expr Expr,
) (TypeDescriptor, map[string]TypeDescriptor, error) {
	if ctx.Resolver == nil {
		return TypeDescriptor{}, nil, xerr.Internal("typed analysis: capability resolver is nil")
	}
	ctx = withPipelineBindingNames(ctx)
	ctx.Bindings = cloneTypeBindings(ctx.Bindings)
	return analyzeTypedExpressionWithBindings(ctx, expr)
}

func analyzeTypedExpressionWithBindings(
	ctx TypeAnalysisContext,
	expr Expr,
) (TypeDescriptor, map[string]TypeDescriptor, error) {
	switch current := expr.(type) {
	case *PipeExpr:
		return analyzeTypedPipeWithBindings(ctx, current)
	case *BindingExpr:
		if err := validateTypedPipelineInputRebinding(ctx, current.Name, current.Pos); err != nil {
			return TypeDescriptor{}, nil, err
		}
		output, bindings, err := analyzeTypedExpressionWithBindings(ctx, current.Inner)
		if err == nil {
			bindings[current.Name] = output
		}
		return output, bindings, err
	case *DecoratedExpr:
		return analyzeTypedExpressionWithBindings(ctx, current.Inner)
	default:
		output, err := analyzeTypedNode(ctx, expr)
		return output, ctx.Bindings, err
	}
}

//nolint:gocyclo // This is the central typed dispatcher; explicit branches preserve boundary checks.
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
		atom, isAtom := node.(*Atom)
		secretContract := contract.SecretBoundary || secret.TypeContains(contract.Input.Type)
		if isAtom && atom != nil {
			atom.SecretBoundary = atom.SecretBoundary || secretContract
			if secret.TypeContains(contract.Input.Type) {
				atom.SecretRequestType = contract.Input.Type
			}
			if secret.TypeContains(contract.Output.Type) {
				atom.SecretResponseType = contract.Output.Type
			}
			if contract.SecretBoundary {
				atom.SecretInputNormalizer = contract.SecretInputNormalizer
			}
		}
		input := ctx.Input
		var err error
		namedPipelineArgs := isAtom && atom != nil &&
			(atom.NamedPipelineArgs || secretContract && atom.HasArgumentObject)
		switch {
		case secretContract && namedPipelineArgs:
			input, err = analyzeTypedNamedPayload(ctx, atom)
		case secretContract && isAtom && atom != nil && atom.DirectInput != nil:
			input, err = analyzeTypedValue(ctx, atom.Pos, atom.DirectInput)
		default:
			if isAtom && atom != nil && hasInjections(atom) && descriptorContainsSecret(input) {
				if input.ContainsSecret && !secret.TypeContains(input.Type) {
					err = secretBoundaryError(atom.Pos, contract.Name)
				} else {
					input.Type = secret.WithoutSecrets(input.Type)
					input.ContainsSecret = false
				}
			} else if descriptorContainsSecret(input) && !secretContract {
				position := Position{}
				if isAtom && atom != nil {
					position = atom.Pos
				}
				err = secretBoundaryError(position, contract.Name)
			}
		}
		runtimeSecretInput := contract.SecretInputNormalizer && input.RuntimeInput && input.Type == nil &&
			!descriptorContainsSecret(input) && secret.TypeContains(contract.Input.Type)
		if err == nil && !runtimeSecretInput {
			if ctx.projectionInput {
				err = requireProjectionShapeCompatible(input, contract.Input, contract.Name)
			} else {
				err = requireCompatible(input, contract.Input, "pipeline value", contract.Name)
			}
		}
		if err == nil && !runtimeSecretInput && secretContract && secret.TypeContains(contract.Input.Type) &&
			!secret.FitsType(input.Type, contract.Input.Type) {
			position := Position{}
			if isAtom && atom != nil && !namedPipelineArgs {
				position = atom.Pos
			}
			return TypeDescriptor{}, secretBoundaryError(position, contract.Name)
		}
		if err != nil {
			return TypeDescriptor{}, err
		}
		if isAtom && atom != nil {
			if err := analyzeTypedAtomValues(ctx, atom); err != nil {
				return TypeDescriptor{}, err
			}
		}
		if isIdentityAction(ctx.Resolver, node) {
			return ctx.Input, nil
		}
		return contract.Output, nil
	}
	return analyzeTypedExpressionNode(ctx, node)
}

func isIdentityAction(resolver CapabilityResolver, node Expr) bool {
	atom, ok := node.(*Atom)
	if !ok || resolver == nil {
		return false
	}
	act, ok := resolver.Action(atom.Name)
	if !ok {
		return false
	}
	meta := act.Describe()
	if meta == nil {
		return false
	}
	// Identity-tagged actions preserve their input's static type.
	return slices.Contains(meta.Tags, "identity")
}

func analyzeTypedPipelineNode(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, bool, error) {
	switch current := node.(type) {
	case *PipeExpr:
		return analyzeTypedPipe(ctx, current)
	case *BindingExpr:
		if err := validateTypedPipelineInputRebinding(ctx, current.Name, current.Pos); err != nil {
			return TypeDescriptor{}, true, err
		}
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
	output, _, err := analyzeTypedPipeWithBindings(ctx, pipe)
	return output, true, err
}

func analyzeTypedPipeWithBindings(
	ctx TypeAnalysisContext,
	pipe *PipeExpr,
) (TypeDescriptor, map[string]TypeDescriptor, error) {
	state := ctx.Input
	localBindings := cloneTypeBindings(ctx.Bindings)
	localBindingNames := clonePipelineBindingNames(ctx.PipelineBindings)
	projectionInput := false
	for _, step := range flattenPipe(pipe) {
		inner, binding := unwrapBinding(step)
		if binding != nil {
			if err := validateTypedPipelineInputRebinding(ctx, binding.Name, binding.Pos); err != nil {
				return TypeDescriptor{}, nil, err
			}
		}
		stepContext := ctx
		stepContext.Input = state
		stepContext.projectionInput = projectionInput
		stepContext.Bindings = localBindings
		stepContext.PipelineBindings = localBindingNames
		output, err := analyzeTypedNode(stepContext, inner)
		if err != nil {
			return TypeDescriptor{}, nil, err
		}
		if binding != nil {
			localBindings[binding.Name] = output
			localBindingNames[binding.Name] = struct{}{}
		}
		state = output
		projectionInput = isProjectionOutput(inner)
	}
	return state, localBindings, nil
}

func isProjectionOutput(node Expr) bool {
	for {
		switch current := node.(type) {
		case *BindingExpr:
			node = current.Inner
		case *DecoratedExpr:
			node = current.Inner
		default:
			_, ok := node.(TypedProjectionOutput)
			return ok
		}
	}
}

func analyzeTypedExpressionNode(ctx TypeAnalysisContext, node Expr) (TypeDescriptor, error) {
	switch current := node.(type) {
	case *Atom:
		return TypeDescriptor{}, analyzeTypedAtomValues(ctx, current)
	case *LiteralExpr:
		if current.Val != nil {
			return TypeDescriptor{Type: reflect.TypeOf(current.Val)}, nil
		}
	case *ValueExpr:
		return analyzeTypedValue(ctx, current.Pos, current.Value)
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
		branchContext := ctx
		branchContext.Bindings = cloneTypeBindings(ctx.Bindings)
		branchContext.PipelineBindings = clonePipelineBindingNames(ctx.PipelineBindings)
		if _, err := analyzeTypedNode(branchContext, branch); err != nil {
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

//nolint:gocyclo // Validates all direct, named, parameter-map, and spread paths for Secret flow.
func analyzeTypedAtomValues(ctx TypeAnalysisContext, atom *Atom) error {
	if atom == nil {
		return nil
	}
	for _, field := range ctx.ArgSchemas[atom.Name] {
		if field.Kind == ArgSecret {
			atom.SecretArgBoundary = true
			break
		}
	}
	if atom.DirectInput != nil {
		descriptor, err := analyzeTypedValue(ctx, atom.Pos, atom.DirectInput)
		if err != nil {
			return err
		}
		if descriptorContainsSecret(descriptor) {
			if !atom.SecretBoundary || !secret.FitsType(descriptor.Type, atom.SecretRequestType) {
				return secretBoundaryError(atom.Pos, atom.Name)
			}
		}
	}
	if atom.NamedPipelineArgs || atom.SecretBoundary && atom.HasArgumentObject {
		descriptor, err := analyzeTypedNamedPayload(ctx, atom)
		if err != nil {
			return err
		}
		if descriptorContainsSecret(descriptor) &&
			(!atom.SecretBoundary || !secret.FitsType(descriptor.Type, atom.SecretRequestType)) {
			return secretBoundaryError(atom.Pos, atom.Name)
		}
		return nil
	}
	kinds := make(map[string]ArgKind, len(ctx.ArgSchemas[atom.Name]))
	for _, field := range ctx.ArgSchemas[atom.Name] {
		kinds[field.Name] = field.Kind
	}
	keys := make([]string, 0, len(atom.Args))
	for key := range atom.Args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		descriptor, err := analyzeTypedValue(ctx, atom.Pos, atom.Args[key])
		if err != nil {
			return err
		}
		kind := kinds[key]
		if kind == ArgSecret {
			if !secret.IsSecretType(descriptor.Type) {
				return SourceError(atom.Pos, "%s.%s: secret parameter requires a statically typed secret value", atom.Name, key)
			}
			continue
		}
		if descriptorContainsSecret(descriptor) {
			return secretBoundaryError(atom.Pos, atom.Name+"."+key)
		}
	}
	paramKeys := make([]string, 0, len(atom.ParamValues))
	for key := range atom.ParamValues {
		paramKeys = append(paramKeys, key)
	}
	slices.Sort(paramKeys)
	for _, key := range paramKeys {
		value := atom.ParamValues[key]
		valueType := reflect.TypeOf(value)
		kind := kinds[key]
		if secret.TypeContains(valueType) {
			if kind != ArgSecret || !secret.IsSecretType(valueType) {
				return secretBoundaryError(atom.Pos, atom.Name+"."+key)
			}
		} else if kind == ArgSecret {
			return SourceError(atom.Pos, "%s.%s: secret parameter requires a typed secret value", atom.Name, key)
		}
	}
	for _, spread := range atom.ArgSpreads {
		descriptor, err := analyzeTypedValue(ctx, atom.Pos, spread)
		if err != nil {
			return err
		}
		if descriptorContainsSecret(descriptor) {
			return SourceError(atom.Pos, "%s: secret values cannot be supplied through argument-map spreads", atom.Name)
		}
		for _, kind := range kinds {
			if kind == ArgSecret {
				return SourceError(atom.Pos, "%s: secret parameters must be passed directly, not through a spread", atom.Name)
			}
		}
	}
	return nil
}

func analyzeTypedNamedPayload(ctx TypeAnalysisContext, atom *Atom) (TypeDescriptor, error) {
	if len(atom.ArgSpreads) > 0 {
		return TypeDescriptor{}, SourceError(atom.Pos, "%s: argument-map spreads are not allowed for a secret-typed pipeline request", atom.Name)
	}
	fields := make([]reflect.StructField, 0, len(atom.Params)+len(atom.ParamValues)+len(atom.Args))
	containsSecret := false
	addField := func(name string, value TypeDescriptor) {
		fieldType := value.Type
		if fieldType == nil {
			fieldType = reflect.TypeFor[any]()
		}
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("Field%d", len(fields)),
			Type: fieldType,
			Tag:  reflect.StructTag("json:" + strconv.Quote(name)),
		})
		containsSecret = containsSecret || descriptorContainsSecret(value)
	}
	paramKeys := make([]string, 0, len(atom.Params))
	for key := range atom.Params {
		paramKeys = append(paramKeys, key)
	}
	slices.Sort(paramKeys)
	for _, key := range paramKeys {
		addField(key, TypeDescriptor{Type: reflect.TypeFor[string]()})
	}
	valueKeys := make([]string, 0, len(atom.ParamValues))
	for key := range atom.ParamValues {
		valueKeys = append(valueKeys, key)
	}
	slices.Sort(valueKeys)
	for _, key := range valueKeys {
		addField(key, TypeDescriptor{Type: reflect.TypeOf(atom.ParamValues[key])})
	}
	argKeys := make([]string, 0, len(atom.Args))
	for key := range atom.Args {
		argKeys = append(argKeys, key)
	}
	slices.Sort(argKeys)
	for _, key := range argKeys {
		value, err := analyzeTypedValue(ctx, atom.Pos, atom.Args[key])
		if err != nil {
			return TypeDescriptor{}, err
		}
		addField(key, value)
	}
	return TypeDescriptor{Type: reflect.StructOf(fields), ContainsSecret: containsSecret}, nil
}

func analyzeTypedValue(ctx TypeAnalysisContext, pos Position, value *Value) (TypeDescriptor, error) {
	if value == nil {
		return TypeDescriptor{}, nil
	}
	return analyzeTypedValueUncached(ctx, pos, value)
}

//nolint:gocyclo // Handles each DSL value form with fail-closed Secret typing rules.
func analyzeTypedValueUncached(ctx TypeAnalysisContext, pos Position, value *Value) (TypeDescriptor, error) {
	if value == nil {
		return TypeDescriptor{}, nil
	}
	var descriptor TypeDescriptor
	switch value.Kind {
	case ValueExpression:
		if ctx.CheckExpression == nil {
			if contextContainsSecret(ctx) {
				return TypeDescriptor{}, SourceError(pos, "secret flow cannot be verified without a typed expression checker")
			}
			return TypeDescriptor{}, nil
		}
		var err error
		descriptor, err = ctx.CheckExpression(value.Expression, ctx, pos)
		if err != nil {
			return TypeDescriptor{}, err
		}
	case ValueRef:
		if value.Ref == "" {
			return ctx.Input, nil
		}
		if strings.HasPrefix(value.Ref, "config.") ||
			strings.HasPrefix(value.Ref, "flag.") || strings.HasPrefix(value.Ref, "arg.") {
			return TypeDescriptor{Type: reflect.TypeFor[string]()}, nil
		}
		if ctx.CheckExpression == nil {
			if contextContainsSecret(ctx) {
				return TypeDescriptor{}, SourceError(pos, "secret flow cannot be verified without a typed expression checker")
			}
			return TypeDescriptor{}, nil
		}
		var err error
		descriptor, err = ctx.CheckExpression(value.Ref, ctx, pos)
		if err != nil {
			return TypeDescriptor{}, err
		}
	case ValueMap:
		fields := make([]reflect.StructField, 0, len(value.Map))
		containsSecret := false
		for index, entry := range value.Map {
			child, err := analyzeTypedValue(ctx, pos, entry.Value)
			if err != nil {
				return TypeDescriptor{}, err
			}
			fieldType := child.Type
			if fieldType == nil {
				fieldType = reflect.TypeFor[any]()
			}
			fields = append(fields, reflect.StructField{
				Name: fmt.Sprintf("Field%d", index), Type: fieldType,
				Tag: reflect.StructTag("json:" + strconv.Quote(entry.Key)),
			})
			containsSecret = containsSecret || descriptorContainsSecret(child)
		}
		descriptor = TypeDescriptor{Type: reflect.StructOf(fields), ContainsSecret: containsSecret}
	case ValueSlice:
		elementType := reflect.TypeFor[any]()
		containsSecret := false
		if len(value.Slice) > 0 {
			first, err := analyzeTypedValue(ctx, pos, value.Slice[0])
			if err != nil {
				return TypeDescriptor{}, err
			}
			if first.Type != nil {
				elementType = first.Type
			}
			containsSecret = descriptorContainsSecret(first)
			for _, item := range value.Slice[1:] {
				current, err := analyzeTypedValue(ctx, pos, item)
				if err != nil {
					return TypeDescriptor{}, err
				}
				containsSecret = containsSecret || descriptorContainsSecret(current)
				if current.Type == nil || current.Type != elementType {
					elementType = reflect.TypeFor[any]()
				}
			}
		}
		descriptor = TypeDescriptor{Type: reflect.SliceOf(elementType), ContainsSecret: containsSecret}
	case ValueTyped:
		descriptor.Type = reflect.TypeOf(value.Typed)
	case ValueString, ValueBare:
		descriptor.Type = reflect.TypeFor[string]()
	case ValueNumber:
		descriptor.Type = reflect.TypeFor[float64]()
	case ValueBool:
		descriptor.Type = reflect.TypeFor[bool]()
	case ValueNull:
		return TypeDescriptor{}, nil
	}
	descriptor.ContainsSecret = descriptor.ContainsSecret || secret.TypeContains(descriptor.Type)
	return descriptor, nil
}

func descriptorContainsSecret(descriptor TypeDescriptor) bool {
	return descriptor.ContainsSecret || secret.TypeContains(descriptor.Type)
}

func contextContainsSecret(ctx TypeAnalysisContext) bool {
	if descriptorContainsSecret(ctx.Input) {
		return true
	}
	if ctx.OriginalInput != nil && descriptorContainsSecret(*ctx.OriginalInput) {
		return true
	}
	for _, descriptor := range ctx.Bindings {
		if descriptorContainsSecret(descriptor) {
			return true
		}
	}
	return false
}

func secretBoundaryError(pos Position, consumer string) error {
	return SourceError(pos, "%s: secret values may only be delivered to a secret-typed action parameter or pipeline request", consumer)
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

func validateInitialBindingCollisions(root Expr, bindings map[string]TypeDescriptor) error {
	if root == nil || len(bindings) == 0 {
		return nil
	}
	switch current := root.(type) {
	case *DecoratedExpr:
		return validateInitialBindingCollisions(current.Inner, bindings)
	case *BindingExpr:
		if _, exists := bindings[current.Name]; exists {
			return SourceError(current.Pos, "duplicate pipeline binding %q", current.Name)
		}
	case *PipeExpr:
		for _, step := range flattenPipe(current) {
			_, binding := unwrapBinding(step)
			if binding != nil {
				if _, exists := bindings[binding.Name]; exists {
					return SourceError(binding.Pos, "duplicate pipeline binding %q", binding.Name)
				}
			}
		}
	}
	return nil
}

func validateTypedPipelineInputRebinding(ctx TypeAnalysisContext, name string, pos Position) error {
	if _, exists := ctx.PipelineInputBindings[name]; exists {
		return SourceError(pos, "duplicate pipeline binding %q", name)
	}
	return nil
}

func cloneTypeBindings(bindings map[string]TypeDescriptor) map[string]TypeDescriptor {
	if len(bindings) == 0 {
		return make(map[string]TypeDescriptor)
	}
	return maps.Clone(bindings)
}

func withPipelineBindingNames(ctx TypeAnalysisContext) TypeAnalysisContext {
	if ctx.PipelineBindings == nil {
		ctx.PipelineBindings = make(map[string]struct{}, len(ctx.Bindings))
		for name := range ctx.Bindings {
			if _, isPipelineInput := ctx.PipelineInputBindings[name]; isPipelineInput {
				continue
			}
			ctx.PipelineBindings[name] = struct{}{}
		}
		return ctx
	}
	ctx.PipelineBindings = clonePipelineBindingNames(ctx.PipelineBindings)
	return ctx
}

func clonePipelineBindingNames(names map[string]struct{}) map[string]struct{} {
	if names == nil {
		return nil
	}
	cloned := make(map[string]struct{}, len(names))
	for name := range names {
		cloned[name] = struct{}{}
	}
	return cloned
}
