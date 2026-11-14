package projection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"

	"github.com/nexssp/flow/compiler"
)

var (
	errProjectionTypeUnknown       = errors.New("projection: static expression type is unknown")
	errProjectionPlaceholderCalled = errors.New("projection: static Flow-value placeholder was called")
)

type projectionBindingType struct {
	name   string
	typeOf compiler.TypeDescriptor
}

func analyzeProjectionTypes(
	raw string,
	pos compiler.Position,
	ctx compiler.TypeAnalysisContext,
	functions *FunctionSet,
	flowBindings []projectionBindingType,
	ctxName string,
) (compiler.TypeDescriptor, error) {
	source, sourceErr := buildProgramSource(raw)
	if sourceErr != nil {
		return compiler.TypeDescriptor{}, compiler.SourceError(pos, "projection: %v", sourceErr)
	}
	source = rewriteProjectionLetsForTypeAnalysis(source)
	if functions != nil {
		if err := functions.prepare(); err != nil {
			return compiler.TypeDescriptor{}, compiler.SourceError(pos, "projection: %v", err)
		}
		source, _ = functions.rewriteCalls(source)
	}

	proxyDepth := projectionExpressionProxyDepth(source)
	env, staticTypes := typedProjectionEnvironment(ctx, proxyDepth)
	if ctxName != "" {
		env[ctxName] = context.Background()
		staticTypes[ctxName] = reflect.TypeFor[context.Context]()
	}
	if err := validateTypedExpressionPaths(source, pos, staticTypes, true); err != nil {
		return compiler.TypeDescriptor{}, err
	}

	options := []expr.Option{spreadMergeOption}
	if len(env) != 0 {
		options = append(options, expr.Env(env))
	}
	options = append(options, expr.AllowUndefinedVariables())
	if functions != nil {
		functionOptions, optionErr := functions.expressionOptions()
		if optionErr != nil {
			return compiler.TypeDescriptor{}, compiler.SourceError(pos, "projection: %v", optionErr)
		}
		options = append(options, functionOptions...)
	}
	for _, binding := range flowBindings {
		outputType := binding.typeOf.Type
		if outputType == nil || binding.typeOf.Stream {
			outputType = reflect.TypeFor[any]()
		}
		outputProxy := projectionStaticType(outputType, proxyDepth, &projectionStaticTypeBuilder{cache: make(map[projectionStaticTypeKey]reflect.Type)})
		analysisFunctionType := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[context.Context](), reflect.TypeFor[any]()},
			[]reflect.Type{outputType, reflect.TypeFor[error]()},
			false,
		)
		expressionFunctionType := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[context.Context](), reflect.TypeFor[any]()},
			[]reflect.Type{outputProxy, reflect.TypeFor[error]()},
			false,
		)
		staticTypes[binding.name] = analysisFunctionType
		options = append(options, expr.Function(
			binding.name,
			func(...any) (any, error) { return nil, errProjectionPlaceholderCalled },
			reflect.New(expressionFunctionType).Interface(),
		))
	}

	program, err := compiler.CompileExpression(source, options...)
	if err != nil {
		if isDynamicExpressionTypeError(err) {
			if pathErr := validateTypedExpressionPaths(source, pos, staticTypes, true); pathErr != nil {
				return compiler.TypeDescriptor{}, pathErr
			}
			if parsed, parseErr := parser.Parse(source); parseErr == nil {
				return compiler.TypeDescriptor{Type: projectionOutputTypeWithStaticTypes(parsed.Node, staticTypes)}, nil
			}
			return compiler.TypeDescriptor{}, nil
		}
		return compiler.TypeDescriptor{}, compiler.SourceError(pos, "projection: %v", err)
	}
	return compiler.TypeDescriptor{Type: projectionOutputTypeWithStaticTypes(program.Node(), staticTypes)}, nil
}

func rewriteProjectionLetsForTypeAnalysis(source string) string {
	_, body, bindings, err := splitLeadingProjectionLets(source)
	if err != nil || len(bindings) == 0 {
		return source
	}
	prefix := "__flow_projection_static_let_"
	for strings.Contains(source, prefix) {
		prefix += "_"
	}
	var rewritten strings.Builder
	renamed := make([]struct{ from, to string }, 0, len(bindings))
	for index, binding := range bindings {
		value := binding.value
		for _, current := range renamed {
			value = replaceProjectionReferences(value, current.from, current.to)
		}
		name := prefix + strconv.Itoa(index) + "__"
		fmt.Fprintf(&rewritten, "let %s = %s; ", name, value)
		renamed = append(renamed, struct{ from, to string }{from: binding.name, to: name})
	}
	for _, current := range renamed {
		body = replaceProjectionReferences(body, current.from, current.to)
	}
	rewritten.WriteString(body)
	return rewritten.String()
}

func replaceProjectionReferences(source, name, replacement string) string {
	scanner := projectionReferenceScanner{name: name}
	var rewritten strings.Builder
	last := 0
	for position := 0; position < len(source); {
		next, found := scanner.advance(source, position)
		if found {
			rewritten.WriteString(source[last:position])
			rewritten.WriteString(replacement)
			last = next
		}
		position = next
	}
	if last == 0 {
		return source
	}
	rewritten.WriteString(source[last:])
	return rewritten.String()
}

// CheckTypedSourceExpression is wired by the projection bundle so core control
// expressions can be checked without coupling the compiler to expr-lang.
func CheckTypedSourceExpression(source string, ctx compiler.TypeAnalysisContext, pos compiler.Position) (compiler.TypeDescriptor, error) {
	source = compiler.PreprocessDots(source)
	env, staticTypes := typedProjectionEnvironment(ctx, projectionExpressionProxyDepth(source))
	program, err := compileTypedExpression(source, env, staticTypes, ctx.ExpressionCompiler, pos)
	if errors.Is(err, errProjectionTypeUnknown) {
		return compiler.TypeDescriptor{}, nil
	}
	if err != nil {
		return compiler.TypeDescriptor{}, compiler.SourceError(pos, "projection: %v", err)
	}
	return compiler.TypeDescriptor{Type: projectionOutputTypeWithStaticTypes(program.Node(), staticTypes)}, nil
}

func typedProjectionEnvironment(ctx compiler.TypeAnalysisContext, proxyDepth int) (env map[string]any, staticTypes map[string]reflect.Type) {
	env = make(map[string]any, len(ctx.Bindings)+4)
	staticTypes = make(map[string]reflect.Type, len(ctx.Bindings)+4)
	builder := &projectionStaticTypeBuilder{cache: make(map[projectionStaticTypeKey]reflect.Type)}
	if ctx.Input.Type != nil && !ctx.Input.Stream {
		root := projectionStaticValueWithBuilder(ctx.Input.Type, proxyDepth, builder)
		env[expressionRootName] = root
		env[expressionStateName] = root
		env["input"] = root
		staticTypes[expressionRootName] = ctx.Input.Type
		staticTypes[expressionStateName] = ctx.Input.Type
		staticTypes["input"] = ctx.Input.Type
		fields := expressionViewFields(ctx.Input.Type)
		for index := range fields {
			field := fields[index]
			value := projectionStaticValueWithBuilder(field.Type, proxyDepth, builder)
			for _, alias := range exactExpressionFieldAliases(field) {
				if projectionFieldIsBuiltin(alias) {
					continue
				}
				setProjectionStaticAlias(env, alias, value)
				setProjectionStaticTypeAlias(staticTypes, alias, field.Type)
			}
		}
		for index := range fields {
			field := fields[index]
			value := projectionStaticValueWithBuilder(field.Type, proxyDepth, builder)
			for _, alias := range generatedExpressionFieldAliases(field) {
				if projectionFieldIsBuiltin(alias) {
					continue
				}
				setProjectionStaticAlias(env, alias, value)
				setProjectionStaticTypeAlias(staticTypes, alias, field.Type)
			}
		}
	}
	for name := range ctx.SymbolNamespaces {
		setProjectionStaticTypeAlias(staticTypes, name, reflect.TypeFor[map[string]any]())
	}
	for name, descriptor := range ctx.Bindings {
		bindingType := descriptor.Type
		if bindingType == nil || descriptor.Stream {
			bindingType = reflect.TypeFor[any]()
		}
		env[name] = projectionStaticValueWithBuilder(bindingType, proxyDepth, builder)
		staticTypes[name] = bindingType
	}
	return env, staticTypes
}

func compileTypedExpression(source string, env map[string]any, staticTypes map[string]reflect.Type, expressionCompiler compiler.ExpressionCompiler, pos compiler.Position) (*vm.Program, error) {
	if err := validateTypedExpressionPaths(source, pos, staticTypes, true); err != nil {
		return nil, err
	}
	if functions, ok := expressionCompiler.(*FunctionSet); ok {
		program, err := compileWithFunctionSet(source, env, functions)
		if err == nil {
			return program, nil
		}
		return dynamicExpressionFallback(source, staticTypes, err, pos)
	}
	options := []expr.Option{expr.Env(env), expr.AllowUndefinedVariables(), spreadMergeOption}
	program, err := compiler.CompileExpression(source, options...)
	if err == nil {
		return program, nil
	}
	if isDynamicExpressionTypeError(err) {
		return dynamicExpressionFallback(source, staticTypes, err, pos)
	}
	if expressionCompiler == nil {
		return nil, err
	}
	// An unrelated source-local compiler may add functions the static compiler
	// cannot see. Keep its syntax validation, but do not pretend it supplied a
	// typed environment when it only accepts an untyped compile request.
	if _, customErr := expressionCompiler.Compile(source, pos); customErr == nil {
		return nil, errProjectionTypeUnknown
	}
	return nil, err
}

func dynamicExpressionFallback(source string, staticTypes map[string]reflect.Type, cause error, pos compiler.Position) (*vm.Program, error) {
	if pathErr := validateTypedExpressionPaths(source, pos, staticTypes, true); pathErr != nil {
		return nil, pathErr
	}
	if isDynamicExpressionTypeError(cause) {
		return nil, errProjectionTypeUnknown
	}
	return nil, cause
}

func isDynamicExpressionTypeError(err error) bool {
	message := strings.ToLower(err.Error())
	if strings.HasPrefix(message, "type nil has no field ") {
		return true
	}
	if !strings.HasPrefix(message, "invalid operation:") {
		return false
	}
	return strings.Contains(message, "(mismatched types unknown and ") ||
		strings.Contains(message, " and unknown)") ||
		strings.Contains(message, "(mismatched type unknown)")
}

func compileWithFunctionSet(source string, env map[string]any, functions *FunctionSet) (*vm.Program, error) {
	if err := functions.prepare(); err != nil {
		return nil, err
	}
	rewritten, _ := functions.rewriteCalls(source)
	functionOptions, err := functions.expressionOptions()
	if err != nil {
		return nil, err
	}
	options := []expr.Option{expr.Env(env), expr.AllowUndefinedVariables(), expr.DisableBuiltin("now"), spreadMergeOption}
	options = append(options, functionOptions...)
	return compiler.CompileExpression(rewritten, options...)
}

func setProjectionStaticAlias(environment map[string]any, name string, value any) {
	if name == "" {
		return
	}
	if _, exists := environment[name]; !exists {
		environment[name] = value
	}
}

func setProjectionStaticTypeAlias(environment map[string]reflect.Type, name string, valueType reflect.Type) {
	if name == "" {
		return
	}
	if _, exists := environment[name]; !exists {
		environment[name] = valueType
	}
}

func projectionStaticFieldType(valueType reflect.Type, selector string) (reflect.Type, bool) {
	fields := expressionViewFields(valueType)
	for index := range fields {
		field := fields[index]
		if slices.Contains(exactExpressionFieldAliases(field), selector) {
			return field.Type, true
		}
	}
	for index := range fields {
		field := fields[index]
		if slices.Contains(generatedExpressionFieldAliases(field), selector) {
			return field.Type, true
		}
	}
	return nil, false
}

func skipProjectionQuoted(raw string, index int) int {
	quote := raw[index]
	for index++; index < len(raw); index++ {
		if raw[index] == '\\' && index+1 < len(raw) {
			index++
			continue
		}
		if raw[index] == quote {
			return index + 1
		}
	}
	return len(raw)
}

func projectionStaticValue(valueType reflect.Type, selectorDepth ...int) any {
	depth := 1
	if len(selectorDepth) > 0 {
		depth = max(selectorDepth[0], 0)
	}
	builder := &projectionStaticTypeBuilder{cache: make(map[projectionStaticTypeKey]reflect.Type)}
	return projectionStaticValueWithBuilder(valueType, depth, builder)
}

func projectionStaticValueWithBuilder(valueType reflect.Type, depth int, builder *projectionStaticTypeBuilder) any {
	proxyType := projectionStaticType(valueType, depth, builder)
	//nolint:exhaustive // Only pointer, map and slice kinds need special runtime values.
	switch proxyType.Kind() {
	case reflect.Pointer:
		if proxyType.Elem().Kind() == reflect.Struct {
			return reflect.New(proxyType.Elem()).Interface()
		}
	case reflect.Map:
		return reflect.MakeMap(proxyType).Interface()
	case reflect.Slice:
		return reflect.MakeSlice(proxyType, 0, 0).Interface()
	}
	return reflect.Zero(proxyType).Interface()
}

type projectionStaticTypeKey struct {
	typeOf          reflect.Type
	selectorDepth   int
	pointerReceiver bool
}

// Keep reflect.StructOf proxy names below Go's internal name-length limit.
// Deeper selectors still use AST validation against original Go types; only
// expr's generated proxy shape becomes dynamic beyond this boundary.
const maxProjectionStaticProxyDepth = 14

type projectionStaticTypeBuilder struct {
	cache map[projectionStaticTypeKey]reflect.Type
}

func projectionStaticType(valueType reflect.Type, selectorDepth int, builder *projectionStaticTypeBuilder) reflect.Type {
	if valueType == nil {
		return reflect.TypeFor[any]()
	}
	if selectorDepth < 0 {
		selectorDepth = 0
	}
	selectorDepth = min(selectorDepth, maxProjectionStaticProxyDepth)
	if selectorDepth == 0 {
		return projectionStaticDynamicType(valueType)
	}
	if isProjectionSchemaType(valueType) {
		return reflect.TypeFor[map[string]any]()
	}
	//nolint:exhaustive // Only pointers and aggregate kinds need proxy reconstruction.
	switch valueType.Kind() {
	case reflect.Pointer:
		element := valueType.Elem()
		if element.Kind() == reflect.Struct {
			return reflect.PointerTo(projectionStaticStruct(element, true, selectorDepth, builder))
		}
		return projectionStaticType(element, selectorDepth, builder)
	case reflect.Slice:
		return reflect.SliceOf(projectionStaticType(valueType.Elem(), selectorDepth-1, builder))
	case reflect.Array:
		return reflect.ArrayOf(valueType.Len(), projectionStaticType(valueType.Elem(), selectorDepth-1, builder))
	case reflect.Map:
		return reflect.MapOf(valueType.Key(), projectionStaticType(valueType.Elem(), selectorDepth-1, builder))
	case reflect.Struct:
		return projectionStaticStruct(valueType, false, selectorDepth, builder)
	case reflect.Func:
		outputs := make([]reflect.Type, valueType.NumOut())
		for index := range outputs {
			outputs[index] = projectionStaticType(valueType.Out(index), selectorDepth-1, builder)
		}
		inputs := make([]reflect.Type, valueType.NumIn())
		for index := range inputs {
			inputs[index] = valueType.In(index)
		}
		return reflect.FuncOf(inputs, outputs, valueType.IsVariadic())
	default:
		return valueType
	}
}

func projectionStaticDynamicType(valueType reflect.Type) reflect.Type {
	//nolint:exhaustive // Only aggregate kinds need dynamic proxies; scalar types retain their concrete type.
	switch valueType.Kind() {
	case reflect.Pointer:
		return projectionStaticDynamicType(valueType.Elem())
	case reflect.Struct:
		return reflect.TypeFor[map[string]any]()
	case reflect.Slice, reflect.Array:
		return reflect.TypeFor[[]any]()
	case reflect.Map:
		return reflect.MapOf(valueType.Key(), reflect.TypeFor[any]())
	case reflect.Func:
		inputs := make([]reflect.Type, valueType.NumIn())
		for index := range inputs {
			inputs[index] = valueType.In(index)
		}
		outputs := make([]reflect.Type, valueType.NumOut())
		for index := range outputs {
			outputs[index] = reflect.TypeFor[any]()
		}
		return reflect.FuncOf(inputs, outputs, valueType.IsVariadic())
	default:
		return valueType
	}
}

func projectionStaticStruct(valueType reflect.Type, pointerReceiver bool, selectorDepth int, builder *projectionStaticTypeBuilder) reflect.Type {
	if selectorDepth == 0 {
		return projectionStaticDynamicType(valueType)
	}
	key := projectionStaticTypeKey{typeOf: valueType, selectorDepth: selectorDepth, pointerReceiver: pointerReceiver}
	if cached, ok := builder.cache[key]; ok {
		return cached
	}

	type staticField struct {
		alias  string
		typeOf reflect.Type
	}
	var aliases []staticField
	seenAliases := make(map[string]struct{})
	add := func(alias string, typ reflect.Type) {
		if alias == "" {
			return
		}
		if _, exists := seenAliases[alias]; exists {
			return
		}
		seenAliases[alias] = struct{}{}
		aliases = append(aliases, staticField{alias: alias, typeOf: typ})
	}
	fields := expressionViewFields(valueType)
	for index := range fields {
		field := fields[index]
		for _, alias := range exactExpressionFieldAliases(field) {
			add(alias, field.Type)
		}
	}
	for index := range fields {
		field := fields[index]
		for _, alias := range generatedExpressionFieldAliases(field) {
			add(alias, field.Type)
		}
	}
	methodType := valueType
	if pointerReceiver {
		methodType = reflect.PointerTo(valueType)
	}
	for method := range methodType.Methods() {
		add(method.Name, boundMethodType(method.Type))
	}
	if len(aliases) == 0 {
		return valueType
	}

	proxyFields := make([]reflect.StructField, 0, len(aliases))
	for index, alias := range aliases {
		proxyFields = append(proxyFields, reflect.StructField{
			Name: fmt.Sprintf("Field%d", index),
			Type: projectionStaticType(alias.typeOf, selectorDepth-1, builder),
			Tag:  reflect.StructTag("expr:" + strconv.Quote(alias.alias) + " flow_projection:\"true\""),
		})
	}
	proxy, err := makeProjectionStructType(proxyFields)
	if err != nil {
		return valueType
	}
	builder.cache[key] = proxy
	return proxy
}

func exactExpressionFieldAliases(field expressionViewField) []string {
	if alias := field.Tag.Get("expr"); alias != "" {
		if field.Tag.Get("flow_projection") == "true" {
			return []string{alias}
		}
		aliases := []string{field.Name, alias}
		if jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ","); jsonName != "" && jsonName != "-" {
			aliases = append(aliases, jsonName)
		}
		return aliases
	}
	aliases := []string{field.Name}
	if jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ","); jsonName != "" && jsonName != "-" {
		aliases = append(aliases, jsonName)
	}
	return aliases
}

func generatedExpressionFieldAliases(field expressionViewField) []string {
	if alias := field.Tag.Get("expr"); alias != "" {
		var aliases []string
		if field.Tag.Get("flow_projection") != "true" {
			if snake := snakeCase(field.Name); snake != field.Name {
				aliases = append(aliases, snake)
			}
		}
		if snake := snakeCase(alias); snake != alias {
			aliases = append(aliases, snake)
		}
		return aliases
	}
	alias := snakeCase(field.Name)
	if alias == field.Name {
		return nil
	}
	return []string{alias}
}

func boundMethodType(methodType reflect.Type) reflect.Type {
	inputs := make([]reflect.Type, methodType.NumIn()-1)
	for i := range inputs {
		inputs[i] = methodType.In(i + 1)
	}
	outputs := make([]reflect.Type, methodType.NumOut())
	for i := range outputs {
		outputs[i] = methodType.Out(i)
	}
	return reflect.FuncOf(inputs, outputs, methodType.IsVariadic())
}

func projectionOutputType(node ast.Node) reflect.Type {
	return projectionOutputTypeWithStaticTypes(node, nil)
}

func projectionOutputTypeWithStaticTypes(node ast.Node, staticTypes map[string]reflect.Type) reflect.Type {
	switch current := node.(type) {
	case *ast.MapNode:
		return projectionMapOutputType(current, staticTypes)
	case *ast.ArrayNode:
		return projectionArrayOutputType(current, staticTypes)
	case *ast.CallNode:
		return projectionSpreadOutputType(current, staticTypes)
	case *ast.ConstantNode:
		return projectionSchemaFromValue(current.Value)
	default:
		if len(staticTypes) > 0 {
			if valueType, known, err := projectionASTNodeType(node, staticTypes); err == nil && known && valueType != nil {
				return valueType
			}
		}
		return projectionLiteralOutputType(node)
	}
}

func projectionMapOutputType(node *ast.MapNode, staticTypes map[string]reflect.Type) reflect.Type {
	fields := make([]reflect.StructField, 0, len(node.Pairs))
	indices := make(map[string]int, len(node.Pairs))
	for _, pairNode := range node.Pairs {
		pair, ok := pairNode.(*ast.PairNode)
		if !ok {
			return reflect.TypeFor[map[string]any]()
		}
		key, ok := projectionStaticKey(pair.Key)
		if !ok {
			return reflect.TypeFor[map[string]any]()
		}
		fieldType := projectionOutputTypeWithStaticTypes(pair.Value, staticTypes)
		if fieldType == nil {
			fieldType = reflect.TypeFor[any]()
		}
		field := reflect.StructField{
			Name: fmt.Sprintf("Field%d", len(fields)),
			Type: fieldType,
			Tag:  reflect.StructTag("expr:" + strconv.Quote(key) + " flow_projection:\"true\""),
		}
		if index, exists := indices[key]; exists {
			field.Name = fields[index].Name
			fields[index] = field
		} else {
			indices[key] = len(fields)
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return reflect.StructOf(nil)
	}
	if schema, err := makeProjectionStructType(fields); err == nil {
		return schema
	}
	return reflect.TypeFor[map[string]any]()
}

func projectionArrayOutputType(node *ast.ArrayNode, staticTypes map[string]reflect.Type) reflect.Type {
	if len(node.Nodes) == 0 {
		return reflect.TypeFor[[]any]()
	}
	elementType := projectionOutputTypeWithStaticTypes(node.Nodes[0], staticTypes)
	for _, element := range node.Nodes[1:] {
		if projectionOutputTypeWithStaticTypes(element, staticTypes) != elementType {
			return reflect.TypeFor[[]any]()
		}
	}
	return reflect.SliceOf(elementType)
}

func projectionSpreadOutputType(node *ast.CallNode, staticTypes map[string]reflect.Type) reflect.Type {
	identifier, ok := node.Callee.(*ast.IdentifierNode)
	if ok && identifier.Value == "__spread__" && len(node.Arguments) == 2 {
		return mergeProjectionSchemaTypes(
			projectionOutputTypeWithStaticTypes(node.Arguments[0], staticTypes),
			projectionOutputTypeWithStaticTypes(node.Arguments[1], staticTypes),
		)
	}
	if len(staticTypes) > 0 {
		if valueType, known, err := projectionASTNodeType(node, staticTypes); err == nil && known && valueType != nil {
			return valueType
		}
	}
	return projectionLiteralOutputType(node)
}

func projectionLiteralOutputType(node ast.Node) reflect.Type {
	switch node.(type) {
	case *ast.IntegerNode:
		return reflect.TypeFor[int]()
	case *ast.FloatNode:
		return reflect.TypeFor[float64]()
	case *ast.BoolNode:
		return reflect.TypeFor[bool]()
	case *ast.StringNode:
		return reflect.TypeFor[string]()
	case *ast.NilNode:
		return reflect.TypeFor[any]()
	}
	if node == nil || node.Type() == nil {
		return reflect.TypeFor[any]()
	}
	return node.Type()
}

func projectionStaticKey(node ast.Node) (string, bool) {
	switch current := node.(type) {
	case *ast.StringNode:
		return current.Value, true
	case *ast.IdentifierNode:
		return current.Value, true
	default:
		return "", false
	}
}

func projectionSchemaFromValue(value any) reflect.Type {
	if object, ok := value.(map[string]any); ok {
		fields := make([]reflect.StructField, 0, len(object))
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for index, key := range keys {
			fieldType := reflect.TypeOf(object[key])
			if fieldType == nil {
				fieldType = reflect.TypeFor[any]()
			}
			fields = append(fields, reflect.StructField{Name: fmt.Sprintf("Field%d", index), Type: projectionSchemaFromValueType(fieldType), Tag: reflect.StructTag("expr:" + strconv.Quote(key) + " flow_projection:\"true\"")})
		}
		if schema, err := makeProjectionStructType(fields); err == nil {
			return schema
		}
	}
	return projectionSchemaFromValueType(reflect.TypeOf(value))
}

func projectionSchemaFromValueType(valueType reflect.Type) reflect.Type {
	if valueType == nil {
		return reflect.TypeFor[any]()
	}
	if valueType.Kind() == reflect.Map && valueType.Key().Kind() == reflect.String {
		return reflect.TypeFor[map[string]any]()
	}
	if valueType.Kind() == reflect.Slice || valueType.Kind() == reflect.Array {
		return reflect.SliceOf(projectionSchemaFromValueType(valueType.Elem()))
	}
	return valueType
}

func mergeProjectionSchemaTypes(base, overlay reflect.Type) reflect.Type {
	baseFields, baseKnown := projectionSchemaFields(base)
	overlayFields, overlayKnown := projectionSchemaFields(overlay)
	if !baseKnown || !overlayKnown {
		return reflect.TypeFor[map[string]any]()
	}
	fields := append([]reflect.StructField(nil), baseFields...)
	indices := make(map[string]int, len(fields)+len(overlayFields))
	for index, field := range fields {
		indices[field.Tag.Get("expr")] = index
	}
	for _, field := range overlayFields {
		key := field.Tag.Get("expr")
		if index, exists := indices[key]; exists {
			fields[index].Type = field.Type
			continue
		}
		field.Name = fmt.Sprintf("Field%d", len(fields))
		indices[key] = len(fields)
		fields = append(fields, field)
	}
	if schema, err := makeProjectionStructType(fields); err == nil {
		return schema
	}
	return reflect.TypeFor[map[string]any]()
}

func projectionSchemaFields(valueType reflect.Type) ([]reflect.StructField, bool) {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return nil, false
	}
	fields := make([]reflect.StructField, 0, valueType.NumField())
	expressionFields := expressionViewFields(valueType)
	for index := range expressionFields {
		field := expressionFields[index]
		aliases := exactExpressionFieldAliases(field)
		if len(aliases) == 0 {
			continue
		}
		if field.Tag.Get("flow_projection") != "true" {
			// A Go struct's runtime view exposes its exact Go and JSON names as
			// separate keys; projectionStaticType already emits each as a proxy.
			continue
		}
		alias := aliases[0]
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("Field%d", len(fields)),
			Type: field.Type,
			Tag:  reflect.StructTag("expr:" + strconv.Quote(alias) + " flow_projection:\"true\""),
		})
	}
	return fields, true
}

func makeProjectionStructType(fields []reflect.StructField) (result reflect.Type, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("projection static type: %v", recovered)
		}
	}()
	return reflect.StructOf(fields), nil
}
