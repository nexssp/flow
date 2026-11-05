package projection

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/internal/identifier"
)

// FunctionParameter describes one typed parameter in a source-local @func.
type FunctionParameter struct {
	Name string
	Type string
}

// FunctionDefinition is the expression-only form of an NFlow @func declaration.
// The projection extension owns expression compilation so calls receive normal
// expr-lang type checking without a process-global function registry.
type FunctionDefinition struct {
	Name       string
	Parameters []FunctionParameter
	Result     string
	Body       string
	Position   compiler.Position
}

// FunctionSet is immutable after construction and can be shared by the
// projections compiled from one source/module. Its cache is deliberately local
// to that source; identical expression text in another source cannot reuse a
// program with different function bindings.
type FunctionSet struct {
	definitions []FunctionDefinition
	byName      map[string]FunctionDefinition
	internal    map[string]string

	once       sync.Once
	programs   map[string]*vm.Program
	prepareErr error
}

// NewFunctionSet copies declarations and assigns deterministic internal names
// for expr-lang, whose custom-function API accepts simple identifiers only.
func NewFunctionSet(definitions []FunctionDefinition) *FunctionSet {
	copied := make([]FunctionDefinition, len(definitions))
	for i, definition := range definitions {
		copied[i] = definition
		copied[i].Parameters = slices.Clone(definition.Parameters)
	}
	sort.Slice(copied, func(i, j int) bool { return copied[i].Name < copied[j].Name })
	set := &FunctionSet{
		definitions: copied,
		byName:      make(map[string]FunctionDefinition, len(copied)),
		internal:    make(map[string]string, len(copied)),
		programs:    make(map[string]*vm.Program, len(copied)),
	}
	for i, definition := range copied {
		set.byName[definition.Name] = definition
		set.internal[definition.Name] = fmt.Sprintf("__nflow_func_%d", i)
	}
	return set
}

// PrimaryWithFunctions returns a projection primary carrying this file-local
// function set. It merges with the ordinary projection primary through the
// compiler's existing MergeablePrimary contract.
func PrimaryWithFunctions(functions *FunctionSet) compiler.PrimaryExtension {
	return Extension{functions: functions}
}

// MergeFunctionSets combines inherited and local declarations without silently
// shadowing a function name. Identical inherited declarations are de-duplicated.
func MergeFunctionSets(older, newer *FunctionSet) *FunctionSet {
	if older == nil {
		return newer
	}
	if newer == nil {
		return older
	}
	definitions := make([]FunctionDefinition, 0, len(older.definitions)+len(newer.definitions))
	seen := make(map[string]FunctionDefinition, cap(definitions))
	for _, definition := range older.definitions {
		definitions = append(definitions, definition)
		seen[definition.Name] = definition
	}
	for _, definition := range newer.definitions {
		if previous, exists := seen[definition.Name]; exists {
			if sameFunctionDefinition(previous, definition) {
				continue
			}
			conflict := NewFunctionSet([]FunctionDefinition{definition})
			conflict.prepareErr = compiler.SourceError(definition.Position,
				"@func %s: duplicate function in inherited and local scopes", definition.Name)
			return conflict
		}
		definitions = append(definitions, definition)
		seen[definition.Name] = definition
	}
	return NewFunctionSet(definitions)
}

func sameFunctionDefinition(left, right FunctionDefinition) bool {
	if left.Name != right.Name || left.Result != right.Result || left.Body != right.Body || len(left.Parameters) != len(right.Parameters) {
		return false
	}
	for i := range left.Parameters {
		if left.Parameters[i] != right.Parameters[i] {
			return false
		}
	}
	return true
}

// Validate compiles all declarations, checking signatures, bodies, references,
// result types, and recursion before a program is built.
func (s *FunctionSet) Validate() error {
	return s.prepare()
}

// Compile rewrites source-visible function names and compiles an expression
// with this immutable source-local function environment.
func (s *FunctionSet) Compile(source string, position compiler.Position) (compiler.ExpressionProgram, error) {
	if err := s.prepare(); err != nil {
		return nil, err
	}
	rewritten, _ := s.rewriteCalls(source)
	options, err := s.expressionOptions()
	if err != nil {
		return nil, err
	}
	compileOptions := append([]expr.Option{expr.AllowUndefinedVariables(), expr.DisableBuiltin("now")}, options...)
	program, err := compiler.CompileExpression(rewritten, compileOptions...)
	if err != nil {
		return nil, compiler.SourceError(position, "invalid expression: %v", err)
	}
	return functionExpressionProgram{program: program}, nil
}

type functionExpressionProgram struct {
	program *vm.Program
}

func (p functionExpressionProgram) Evaluate(ctx context.Context, input any) (any, error) {
	return p.EvaluateEnvironment(ctx, compiler.BuildEnv(input))
}

func (p functionExpressionProgram) EvaluateEnvironment(ctx context.Context, environment map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env := compiler.MergeSymbolNamespaces(ctx, environment)
	result, err := expr.Run(p.program, env)
	if err != nil {
		return nil, xerr.BadRequest("expression evaluation failed", err)
	}
	return result, nil
}

// RewriteFunctionReferences replaces qualified function names while preserving
// quoted text and ordinary member access. It is used when an imported module's
// local function names are mounted below its @include namespace.
func RewriteFunctionReferences(source string, references map[string]string) string {
	if len(references) == 0 {
		return source
	}
	var output strings.Builder
	output.Grow(len(source))
	for index := 0; index < len(source); {
		if source[index] == '"' || source[index] == '\'' || source[index] == '`' {
			index = copyFunctionQuoted(&output, source, index)
			continue
		}
		if identifier.IsStartByte(source[index]) && (index == 0 || source[index-1] != '.' && !identifier.IsContinueByte(source[index-1])) {
			end := scanQualifiedIdentifier(source, index)
			name := source[index:end]
			next := end
			for next < len(source) && isFunctionWhitespace(source[next]) {
				next++
			}
			if replacement, ok := references[name]; ok && next < len(source) && source[next] == '(' {
				output.WriteString(replacement)
				index = end
				continue
			}
		}
		output.WriteByte(source[index])
		index++
	}
	return output.String()
}

func (s *FunctionSet) prepare() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		s.prepareErr = s.compileFunctions()
	})
	return s.prepareErr
}

func (s *FunctionSet) compileFunctions() error {
	if s.prepareErr != nil {
		return s.prepareErr
	}
	for index := 1; index < len(s.definitions); index++ {
		if s.definitions[index-1].Name == s.definitions[index].Name {
			definition := s.definitions[index]
			return compiler.SourceError(definition.Position, "@func %s: duplicate visible function name", definition.Name)
		}
	}
	for _, definition := range s.definitions {
		if err := validateFunctionSignature(definition); err != nil {
			return compiler.SourceError(definition.Position, "@func %s: %v", definition.Name, err)
		}
	}
	if err := s.rejectRecursion(); err != nil {
		return err
	}
	options, err := s.expressionOptions()
	if err != nil {
		return err
	}
	for _, definition := range s.definitions {
		rewritten, _ := s.rewriteCalls(definition.Body)
		env := make(map[string]any, len(definition.Parameters))
		for _, parameter := range definition.Parameters {
			typeOf, _ := functionType(parameter.Type)
			env[parameter.Name] = reflect.Zero(typeOf).Interface()
		}
		compileOptions := append([]expr.Option{expr.Env(env), expr.DisableBuiltin("now")}, options...)
		compileOptions = append(compileOptions, resultTypeOption(definition.Result))
		program, compileErr := compiler.CompileExpression(rewritten, compileOptions...)
		if compileErr != nil {
			return compiler.SourceError(definition.Position, "@func %s: invalid expression: %v", definition.Name, compileErr)
		}
		s.programs[definition.Name] = program
	}
	return nil
}

func validateFunctionSignature(definition FunctionDefinition) error {
	if !identifier.IsQualified(definition.Name) {
		return fmt.Errorf("invalid name %q; use namespace.function", definition.Name)
	}
	if _, ok := functionType(definition.Result); !ok {
		return fmt.Errorf("unsupported result type %q", definition.Result)
	}
	seen := make(map[string]struct{}, len(definition.Parameters))
	for _, parameter := range definition.Parameters {
		if !identifier.IsValid(parameter.Name) {
			return fmt.Errorf("invalid parameter name %q", parameter.Name)
		}
		if _, duplicate := seen[parameter.Name]; duplicate {
			return fmt.Errorf("duplicate parameter %q", parameter.Name)
		}
		seen[parameter.Name] = struct{}{}
		if _, ok := functionType(parameter.Type); !ok {
			return fmt.Errorf("unsupported parameter type %q for %q", parameter.Type, parameter.Name)
		}
	}
	return nil
}

func (s *FunctionSet) expressionOptions() ([]expr.Option, error) {
	options := make([]expr.Option, 0, len(s.definitions))
	for _, definition := range s.definitions {
		parameterTypes := make([]reflect.Type, len(definition.Parameters))
		for i, parameter := range definition.Parameters {
			typeOf, ok := functionType(parameter.Type)
			if !ok {
				return nil, compiler.SourceError(definition.Position, "@func %s: unsupported parameter type %q", definition.Name, parameter.Type)
			}
			parameterTypes[i] = typeOf
		}
		resultType, ok := functionType(definition.Result)
		if !ok {
			return nil, compiler.SourceError(definition.Position, "@func %s: unsupported result type %q", definition.Name, definition.Result)
		}
		signature := reflect.FuncOf(parameterTypes, []reflect.Type{resultType}, false)
		options = append(options, expr.Function(
			s.internal[definition.Name],
			s.makeInvoker(definition),
			reflect.Zero(signature).Interface(),
		))
	}
	return options, nil
}

func (s *FunctionSet) makeInvoker(definition FunctionDefinition) func(...any) (any, error) {
	return func(arguments ...any) (any, error) {
		if len(arguments) != len(definition.Parameters) {
			return nil, xerr.Validation(fmt.Sprintf("@func %s: expected %d arguments, got %d", definition.Name, len(definition.Parameters), len(arguments)))
		}
		program := s.programs[definition.Name]
		if program == nil {
			return nil, xerr.Internal("@func " + definition.Name + ": compiled body is unavailable")
		}
		env := make(map[string]any, len(arguments))
		for i, parameter := range definition.Parameters {
			typeOf, _ := functionType(parameter.Type)
			value, err := coerceFunctionValue(arguments[i], typeOf)
			if err != nil {
				return nil, xerr.Validation(fmt.Sprintf("@func %s: parameter %q: %v", definition.Name, parameter.Name, err))
			}
			env[parameter.Name] = value
		}
		result, err := expr.Run(program, env)
		if err != nil {
			return nil, xerr.BadRequest("@func "+definition.Name+" evaluation failed", err)
		}
		resultType, _ := functionType(definition.Result)
		converted, err := coerceFunctionValue(result, resultType)
		if err != nil {
			return nil, xerr.Validation(fmt.Sprintf("@func %s: result: %v", definition.Name, err))
		}
		return converted, nil
	}
}

func (s *FunctionSet) rejectRecursion() error {
	state := make(map[string]uint8, len(s.definitions))
	var visit func(string, []string) error
	visit = func(name string, stack []string) error {
		switch state[name] {
		case 1:
			definition := s.byName[name]
			cycle := slices.Clone(stack)
			cycle = append(cycle, name)
			return compiler.SourceError(definition.Position, "@func %s: recursive function cycle is not supported (%s)", name, strings.Join(cycle, " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		definition := s.byName[name]
		_, calls := s.rewriteCalls(definition.Body)
		nextStack := slices.Clone(stack)
		nextStack = append(nextStack, name)
		for _, called := range calls {
			if err := visit(called, nextStack); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for _, definition := range s.definitions {
		if err := visit(definition.Name, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *FunctionSet) rewriteCalls(source string) (rewritten string, calls []string) {
	if s == nil || len(s.definitions) == 0 {
		return source, nil
	}
	var output strings.Builder
	output.Grow(len(source))
	for index := 0; index < len(source); {
		if source[index] == '"' || source[index] == '\'' || source[index] == '`' {
			index = copyFunctionQuoted(&output, source, index)
			continue
		}
		if identifier.IsStartByte(source[index]) && (index == 0 || source[index-1] != '.' && !identifier.IsContinueByte(source[index-1])) {
			end := scanQualifiedIdentifier(source, index)
			name := source[index:end]
			next := end
			for next < len(source) && isFunctionWhitespace(source[next]) {
				next++
			}
			if internal, ok := s.internal[name]; ok && next < len(source) && source[next] == '(' {
				output.WriteString(internal)
				calls = append(calls, name)
				index = end
				continue
			}
		}
		output.WriteByte(source[index])
		index++
	}
	return output.String(), calls
}

func copyFunctionQuoted(output *strings.Builder, source string, index int) int {
	quote := source[index]
	output.WriteByte(quote)
	index++
	for index < len(source) {
		current := source[index]
		output.WriteByte(current)
		index++
		if current == '\\' && quote != '`' && index < len(source) {
			output.WriteByte(source[index])
			index++
			continue
		}
		if current == quote {
			break
		}
	}
	return index
}

func scanQualifiedIdentifier(source string, start int) int {
	index := start
	for index < len(source) {
		if !identifier.IsStartByte(source[index]) {
			return index
		}
		index++
		for index < len(source) && identifier.IsContinueByte(source[index]) {
			index++
		}
		if index+1 >= len(source) || source[index] != '.' || !identifier.IsStartByte(source[index+1]) {
			return index
		}
		index++
	}
	return index
}

func functionType(name string) (reflect.Type, bool) {
	switch name {
	case "int":
		return reflect.TypeFor[int](), true
	case "int64":
		return reflect.TypeFor[int64](), true
	case "float64":
		return reflect.TypeFor[float64](), true
	case "string":
		return reflect.TypeFor[string](), true
	case "bool":
		return reflect.TypeFor[bool](), true
	default:
		return nil, false
	}
}

func resultTypeOption(name string) expr.Option {
	typeOf, _ := functionType(name)
	return expr.AsKind(typeOf.Kind())
}

func coerceFunctionValue(value any, target reflect.Type) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("cannot use null as %s", target)
	}
	source := reflect.ValueOf(value)
	if source.Type().AssignableTo(target) {
		return value, nil
	}
	if !source.Type().ConvertibleTo(target) {
		return nil, fmt.Errorf("got %s, want %s", source.Type(), target)
	}
	switch target.Kind() {
	case reflect.Int, reflect.Int64:
		number, ok := numericFloat64(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return nil, fmt.Errorf("got %T, want an integer %s", value, target)
		}
		bits := target.Bits()
		if bits == 64 {
			if number < math.MinInt64 || number >= math.MaxInt64 {
				return nil, fmt.Errorf("integer %v is outside %s range", number, target)
			}
		} else {
			minimum := -math.Pow(2, float64(bits-1))
			maximum := math.Pow(2, float64(bits-1))
			if number < minimum || number >= maximum {
				return nil, fmt.Errorf("integer %v is outside %s range", number, target)
			}
		}
		return source.Convert(target).Interface(), nil
	case reflect.Float64:
		if source.Kind() < reflect.Int || source.Kind() > reflect.Float64 {
			return nil, fmt.Errorf("got %T, want float64", value)
		}
		return source.Convert(target).Interface(), nil
	case reflect.String, reflect.Bool:
		return nil, fmt.Errorf("got %T, want %s", value, target)
	case reflect.Invalid, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Complex64,
		reflect.Complex128, reflect.Array, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice,
		reflect.Struct, reflect.UnsafePointer:
		return nil, fmt.Errorf("unsupported type %s", target)
	default:
		return nil, fmt.Errorf("unsupported type %s", target)
	}
}

func numericFloat64(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func isFunctionWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}
