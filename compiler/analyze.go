package compiler

import (
	"encoding"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/secret"
)

type TypeDescriptor struct {
	Type           reflect.Type
	Stream         bool
	ContainsSecret bool
	RuntimeInput   bool
}

type CapabilityContract struct {
	Name                  string
	Input                 TypeDescriptor
	Output                TypeDescriptor
	SecretBoundary        bool
	SecretInputNormalizer bool
}

type ContractProvider interface {
	Contract(name string) (CapabilityContract, bool)
}

type ContractRegistrar interface {
	RegisterContract(name string, contract CapabilityContract) error
}

// Analyze runs polymorphic contract validation on the AST. It is a
// compile-time gate: any edge whose producer output cannot be coerced
// into the consumer input fails here, before Execute is ever reached.
//
// Nominally assignable payloads pass directly. Structural JSON coercions are
// checked conservatively so known value-dependent failures surface at compile time.
func Analyze(resolver CapabilityResolver, expr Expr) error {
	return AnalyzeWithTypedExpressionChecker(resolver, expr, nil, nil)
}

// AnalyzeWithTypedExpressionChecker runs ordinary AST analysis followed by
// type propagation and extension-provided typed expression validation.
func AnalyzeWithTypedExpressionChecker(
	resolver CapabilityResolver,
	expr Expr,
	checker TypedSourceExpressionChecker,
	expressionCompiler ExpressionCompiler,
	symbolNamespaces ...map[string]SymbolNamespace,
) error {
	return AnalyzeWithTypedExpressionCheckerAndInput(
		resolver, expr, checker, expressionCompiler, TypeDescriptor{}, symbolNamespaces...,
	)
}

// AnalyzeWithTypedExpressionCheckerAndInput runs typed analysis with an initial
// payload type, in addition to the expression checker and local symbols.
func AnalyzeWithTypedExpressionCheckerAndInput(
	resolver CapabilityResolver,
	expr Expr,
	checker TypedSourceExpressionChecker,
	expressionCompiler ExpressionCompiler,
	input TypeDescriptor,
	symbolNamespaces ...map[string]SymbolNamespace,
) error {
	return AnalyzeWithTypedExpressionCheckerAndInputAndArgSchemas(
		resolver, expr, checker, expressionCompiler, input, nil, symbolNamespaces...,
	)
}

// AnalyzeWithTypedExpressionCheckerAndInputAndArgSchemas also supplies each
// action's declared parameter kinds to secret-flow analysis.
func AnalyzeWithTypedExpressionCheckerAndInputAndArgSchemas(
	resolver CapabilityResolver,
	expr Expr,
	checker TypedSourceExpressionChecker,
	expressionCompiler ExpressionCompiler,
	input TypeDescriptor,
	argSchemas map[string][]ArgFieldSpec,
	symbolNamespaces ...map[string]SymbolNamespace,
) error {
	return analyzeWithInitialBindings(
		resolver, expr, checker, expressionCompiler, input, argSchemas, nil, symbolNamespaces...,
	)
}

func analyzeWithInitialBindings(
	resolver CapabilityResolver,
	expr Expr,
	checker TypedSourceExpressionChecker,
	expressionCompiler ExpressionCompiler,
	input TypeDescriptor,
	argSchemas map[string][]ArgFieldSpec,
	initialBindings map[string]TypeDescriptor,
	symbolNamespaces ...map[string]SymbolNamespace,
) error {
	if resolver == nil {
		return xerr.Internal("analyze: capability resolver is nil")
	}
	if expr == nil {
		return nil
	}
	if err := expr.Analyze(resolver); err != nil {
		return err
	}
	if err := validateInitialBindingCollisions(expr, initialBindings); err != nil {
		return err
	}
	var localSymbols map[string]SymbolNamespace
	if len(symbolNamespaces) > 0 {
		localSymbols = symbolNamespaces[0]
	}
	originalInput := input
	return analyzeTypedExpressions(TypeAnalysisContext{
		Resolver:           resolver,
		Input:              input,
		OriginalInput:      &originalInput,
		Bindings:           cloneTypeBindings(initialBindings),
		SymbolNamespaces:   localSymbols,
		CheckExpression:    checker,
		ExpressionCompiler: expressionCompiler,
		ArgSchemas:         argSchemas,
	}, expr)
}

func contractForExpr(resolver CapabilityResolver, expr Expr) (CapabilityContract, bool) {
	atom, ok := expr.(*Atom)
	if !ok || atom == nil {
		return CapabilityContract{}, false
	}
	if provider, ok := resolver.(ContractProvider); ok {
		if contract, found := provider.Contract(atom.Name); found {
			return contract, true
		}
	}
	if act, found := resolver.Action(atom.Name); found {
		return actionContract(atom.Name, act), true
	}
	if src, found := resolver.Stream(atom.Name); found {
		return CapabilityContract{
			Name:   atom.Name,
			Input:  TypeDescriptor{Type: payloadType(src.ReqPayload())},
			Output: TypeDescriptor{Type: payloadType(src.ResPayload()), Stream: true},
		}, true
	}
	if op, found := resolver.Operator(atom.Name); found {
		return CapabilityContract{
			Name:   atom.Name,
			Input:  TypeDescriptor{Type: op.InType, Stream: true},
			Output: TypeDescriptor{Type: op.OutType, Stream: true},
		}, true
	}
	return CapabilityContract{}, false
}

func actionContract(name string, act action.AnyAction) CapabilityContract {
	contract := CapabilityContract{Name: name}
	if typed, ok := act.(action.TypedPayload); ok {
		contract.Input.Type = payloadType(typed.ReqPayload())
		contract.Output.Type = payloadType(typed.ResPayload())
	}
	return contract
}

func ActionContract(name string, act action.AnyAction) CapabilityContract {
	return actionContract(name, act)
}

func payloadType(payload any) reflect.Type {
	if payload == nil {
		return nil
	}
	return reflect.TypeOf(payload)
}

// requireCompatible validates one pipeline edge. It rejects type-dependent
// conversions before probing the structural JSON fallback used by action.Coerce.
func requireCompatible(produced, consumed TypeDescriptor, producer, consumer string) error {
	hasSecret := produced.ContainsSecret || secret.TypeContains(produced.Type)
	if produced.Stream != consumed.Stream {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer +
				": stream and unary payloads are incompatible")
	}
	if hasSecret {
		if produced.Type == nil || consumed.Type == nil || produced.ContainsSecret && !secret.TypeContains(produced.Type) || !secret.FitsType(produced.Type, consumed.Type) {
			return xerr.Validation(
				"contract mismatch " + producer + " -> " + consumer +
					": payload containing a secret requires a matching secret-typed contract")
		}
		producedWire, consumedWire := secret.WireType(produced.Type), secret.WireType(consumed.Type)
		if producedWire == nil || consumedWire == nil {
			return xerr.Validation("contract mismatch " + producer + " -> " + consumer + ": cannot validate secret-bearing JSON shape")
		}
		if err := valueDependentCoercion(producedWire, consumedWire); err != nil {
			return xerr.Validation("contract mismatch " + producer + " -> " + consumer + ": " + err.Error())
		}
		if err := coerceCheck(producedWire, consumedWire); err != nil {
			return xerr.Validation("contract mismatch " + producer + " -> " + consumer + ": " + err.Error())
		}
		return nil
	}
	if produced.Type == nil || consumed.Type == nil {
		return nil
	}
	if produced.Type.AssignableTo(consumed.Type) {
		return nil
	}
	if runtimeCoercionIsGuaranteed(produced.Type, consumed.Type) {
		return nil
	}
	if produced.Type.Kind() == reflect.Interface {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer +
				": dynamic output type " + produced.Type.String() +
				" is not guaranteed compatible with " + consumed.Type.String())
	}
	if err := valueDependentCoercion(produced.Type, consumed.Type); err != nil {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer + ": " + err.Error())
	}
	if err := coerceCheck(produced.Type, consumed.Type); err != nil {
		return xerr.Validation(
			"contract mismatch " + producer + " -> " + consumer + ": " + err.Error())
	}
	return nil
}

func requireProjectionShapeCompatible(produced, consumed TypeDescriptor, consumer string) error {
	if produced.ContainsSecret || secret.TypeContains(produced.Type) {
		return requireCompatible(produced, consumed, "pipeline value", consumer)
	}
	if produced.Stream != consumed.Stream {
		return requireCompatible(produced, consumed, "pipeline value", consumer)
	}
	mismatch := findProjectionTypeShapeMismatch(
		produced.Type,
		consumed.Type,
		"",
		make(map[coercionTypePair]struct{}),
	)
	if mismatch == nil {
		return nil
	}
	detail := fmt.Sprintf("JSON value type %s is not compatible with %s", mismatch.produced, mismatch.consumed)
	if mismatch.field != "" {
		detail = fmt.Sprintf("field %q: %s", mismatch.field, detail)
	}
	return xerr.Validation(fmt.Sprintf("contract mismatch pipeline value -> %s: %s", consumer, detail))
}

type projectionTypeShapeMismatch struct {
	field    string
	produced reflect.Type
	consumed reflect.Type
}

func findProjectionTypeShapeMismatch(
	produced, consumed reflect.Type,
	field string,
	seen map[coercionTypePair]struct{},
) *projectionTypeShapeMismatch {
	if produced == nil || consumed == nil {
		return nil
	}
	for produced.Kind() == reflect.Pointer {
		produced = produced.Elem()
	}
	for consumed.Kind() == reflect.Pointer {
		consumed = consumed.Elem()
	}
	if isJSONValueType(consumed) && isJSONValueSourceShape(produced) {
		return nil
	}
	if produced.AssignableTo(consumed) {
		return nil
	}
	if produced.Kind() == reflect.Interface {
		if consumed.Kind() == reflect.Interface && consumed.NumMethod() == 0 {
			return nil
		}
		return &projectionTypeShapeMismatch{field: field, produced: produced, consumed: consumed}
	}
	if consumed.Kind() == reflect.Interface {
		if consumed.NumMethod() == 0 {
			return nil
		}
		return &projectionTypeShapeMismatch{field: field, produced: produced, consumed: consumed}
	}
	// Matching JSON-number shapes do not guarantee a safe conversion.
	if isNumericKind(produced.Kind()) && isNumericKind(consumed.Kind()) {
		if err := valueDependentCoercion(produced, consumed); err != nil {
			return &projectionTypeShapeMismatch{field: field, produced: produced, consumed: consumed}
		}
		return nil
	}
	pair := coercionTypePair{produced: produced, consumed: consumed}
	if _, ok := seen[pair]; ok {
		return nil
	}
	seen[pair] = struct{}{}
	defer delete(seen, pair)

	if jsonShape(produced) != jsonShape(consumed) {
		return &projectionTypeShapeMismatch{field: field, produced: produced, consumed: consumed}
	}
	//nolint:exhaustive // only JSON container kinds need nested shape comparison
	switch produced.Kind() {
	case reflect.Struct:
		return findProjectionStructShapeMismatch(produced, consumed, field, seen)
	case reflect.Array, reflect.Slice:
		return findProjectionSequenceShapeMismatch(produced, consumed, field, seen)
	}
	return nil
}

func findProjectionStructShapeMismatch(
	produced, consumed reflect.Type,
	field string,
	seen map[coercionTypePair]struct{},
) *projectionTypeShapeMismatch {
	producedFields, err := jsonTypeFields(produced)
	if err != nil {
		return nil
	}
	//nolint:exhaustive // only object-shaped JSON targets need field comparison
	switch consumed.Kind() {
	case reflect.Struct:
		return findProjectionStructFieldsShapeMismatch(producedFields, consumed, field, seen)
	case reflect.Map:
		return findProjectionStructMapShapeMismatch(producedFields, consumed, field, seen)
	}
	return nil
}

func findProjectionStructFieldsShapeMismatch(
	producedFields []jsonTypeField,
	consumed reflect.Type,
	field string,
	seen map[coercionTypePair]struct{},
) *projectionTypeShapeMismatch {
	consumedFields, err := jsonTypeFields(consumed)
	if err != nil {
		return nil
	}
	for _, producedField := range producedFields {
		consumedField, ok, err := findJSONTypeField(consumedFields, producedField.name)
		if err != nil || !ok {
			continue
		}
		if mismatch := findProjectionTypeShapeMismatch(
			producedField.typeOf,
			consumedField.typeOf,
			projectionFieldPath(field, producedField.name),
			seen,
		); mismatch != nil {
			return mismatch
		}
	}
	return nil
}

func findProjectionStructMapShapeMismatch(
	producedFields []jsonTypeField,
	consumed reflect.Type,
	field string,
	seen map[coercionTypePair]struct{},
) *projectionTypeShapeMismatch {
	stringType := reflect.TypeFor[string]()
	if !jsonMapKeyCanUnmarshal(consumed.Key()) || !jsonMapKeysCompatible(stringType, consumed.Key()) {
		return &projectionTypeShapeMismatch{field: field, produced: stringType, consumed: consumed.Key()}
	}
	for _, producedField := range producedFields {
		if mismatch := findProjectionTypeShapeMismatch(
			producedField.typeOf,
			consumed.Elem(),
			projectionFieldPath(field, producedField.name),
			seen,
		); mismatch != nil {
			return mismatch
		}
	}
	return nil
}

func findProjectionSequenceShapeMismatch(
	produced, consumed reflect.Type,
	field string,
	seen map[coercionTypePair]struct{},
) *projectionTypeShapeMismatch {
	if consumed.Kind() != reflect.Array && consumed.Kind() != reflect.Slice {
		return nil
	}
	return findProjectionTypeShapeMismatch(produced.Elem(), consumed.Elem(), field+"[]", seen)
}

func projectionFieldPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

type coercionTypePair struct {
	produced reflect.Type
	consumed reflect.Type
}

type jsonTypeField struct {
	name        string
	typeOf      reflect.Type
	depth       int
	tagged      bool
	caseStrict  bool
	stringQuote bool
}

const jsonTagStringOption = stringKindName

type jsonTypeFieldGroup struct {
	depth         int
	tagged        jsonTypeField
	taggedCount   int
	untagged      jsonTypeField
	untaggedCount int
}

func valueDependentCoercion(produced, consumed reflect.Type) error {
	return valueDependentCoercionSeen(produced, consumed, make(map[coercionTypePair]struct{}))
}

func valueDependentCoercionSeen(produced, consumed reflect.Type, seen map[coercionTypePair]struct{}) error {
	if produced == nil || consumed == nil || produced.AssignableTo(consumed) {
		return nil
	}
	pair := coercionTypePair{produced: produced, consumed: consumed}
	if _, ok := seen[pair]; ok {
		return nil
	}
	seen[pair] = struct{}{}

	produced, consumed, needsInspection, err := prepareJSONCoercion(produced, consumed)
	if err != nil {
		return err
	}
	if !needsInspection {
		return nil
	}
	return inspectJSONCoercion(produced, consumed, seen)
}

func prepareJSONCoercion(produced, consumed reflect.Type) (normalizedProduced, normalizedConsumed reflect.Type, needsInspection bool, err error) {
	if consumed.Kind() == reflect.Interface {
		if consumed.NumMethod() == 0 {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("JSON cannot guarantee a value for interface %s", consumed)
	}
	if produced.Kind() == reflect.Interface {
		return nil, nil, false, fmt.Errorf("dynamic value type %s is not guaranteed compatible with %s", produced, consumed)
	}
	for produced.Kind() == reflect.Pointer {
		produced = produced.Elem()
	}
	for consumed.Kind() == reflect.Pointer {
		consumed = consumed.Elem()
	}
	if produced == consumed || isJSONValueType(consumed) && isJSONValueSourceShape(produced) {
		return nil, nil, false, nil
	}
	if hasCustomJSONMethod(produced, true) || hasCustomJSONMethod(consumed, false) {
		return nil, nil, false, fmt.Errorf("custom JSON codec on %s or %s prevents guaranteed compatibility", produced, consumed)
	}
	if produced.Kind() == reflect.String && isByteSlice(consumed) {
		return nil, nil, false, fmt.Errorf("JSON base64 decoding from %s to %s is not guaranteed", produced, consumed)
	}
	producedShape, consumedShape := jsonShape(produced), jsonShape(consumed)
	if producedShape == reflect.Invalid || producedShape != consumedShape {
		return nil, nil, false, fmt.Errorf("JSON value type %s is not guaranteed compatible with %s", produced, consumed)
	}

	if isNumericKind(produced.Kind()) && isNumericKind(consumed.Kind()) && !numericCoercionIsSafe(produced, consumed) {
		return nil, nil, false, fmt.Errorf("value-dependent numeric coercion from %s to %s is not guaranteed", produced, consumed)
	}
	return produced, consumed, true, nil
}

func runtimeCoercionIsGuaranteed(produced, consumed reflect.Type) bool {
	if consumed.Kind() == reflect.String {
		return produced.Implements(reflect.TypeFor[fmt.Stringer]()) || produced.Implements(reflect.TypeFor[error]())
	}
	return consumed == reflect.TypeFor[[]byte]() &&
		(produced == reflect.TypeFor[string]() || produced.Implements(reflect.TypeFor[fmt.Stringer]()))
}

// isJSONValueType reports whether valueType is encoding/json/jsontext.Value,
// including a pointer wrapper.
func isJSONValueType(valueType reflect.Type) bool {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType == reflect.TypeFor[jsontext.Value]()
}

func isJSONValueSourceShape(valueType reflect.Type) bool {
	// Kernel's slice coercion can reinterpret numeric slices as bytes instead
	// of JSON-encoding them, so only object and scalar shapes are guaranteed.
	switch valueType.Kind() {
	case reflect.Struct, reflect.Map, reflect.Bool, reflect.String:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Invalid, reflect.Array, reflect.Chan, reflect.Complex64, reflect.Complex128,
		reflect.Func, reflect.Interface, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return false
	}
	return false
}

func jsonShape(valueType reflect.Type) reflect.Kind {
	switch valueType.Kind() {
	case reflect.Struct, reflect.Map:
		return reflect.Map
	case reflect.Array, reflect.Slice:
		if isByteSlice(valueType) {
			return reflect.String
		}
		return reflect.Slice
	case reflect.Bool, reflect.String:
		return valueType.Kind()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return reflect.Float64
	case reflect.Invalid, reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer,
		reflect.UnsafePointer, reflect.Complex64, reflect.Complex128:
		return reflect.Invalid
	}
	return reflect.Invalid
}

func isByteSlice(valueType reflect.Type) bool {
	return valueType.Kind() == reflect.Slice && valueType.Elem().Kind() == reflect.Uint8
}

func hasCustomJSONMethod(valueType reflect.Type, marshaler bool) bool {
	var methods []reflect.Type
	if marshaler {
		methods = []reflect.Type{
			reflect.TypeFor[interface {
				MarshalJSONTo(*jsontext.Encoder) error
			}](),
			reflect.TypeFor[json.Marshaler](),
			reflect.TypeFor[encoding.TextAppender](),
			reflect.TypeFor[encoding.TextMarshaler](),
		}
	} else {
		methods = []reflect.Type{
			reflect.TypeFor[interface {
				UnmarshalJSONFrom(*jsontext.Decoder) error
			}](),
			reflect.TypeFor[json.Unmarshaler](),
			reflect.TypeFor[encoding.TextUnmarshaler](),
		}
	}
	for _, method := range methods {
		if valueType.Implements(method) ||
			valueType.Kind() != reflect.Pointer && reflect.PointerTo(valueType).Implements(method) {
			return true
		}
	}
	return false
}

func inspectJSONCoercion(produced, consumed reflect.Type, seen map[coercionTypePair]struct{}) error {
	//nolint:exhaustive // only JSON container kinds need nested inspection
	switch produced.Kind() {
	case reflect.Struct:
		return inspectStructCoercion(produced, consumed, seen)
	case reflect.Map:
		return inspectMapCoercion(produced, consumed, seen)
	case reflect.Array, reflect.Slice:
		return inspectSequenceCoercion(produced, consumed, seen)
	default:
		return nil
	}
}

func inspectStructCoercion(produced, consumed reflect.Type, seen map[coercionTypePair]struct{}) error {
	producedFields, err := jsonTypeFields(produced)
	if err != nil {
		return err
	}
	if consumed.Kind() == reflect.Struct {
		consumedFields, err := jsonTypeFields(consumed)
		if err != nil {
			return err
		}
		for _, producedField := range producedFields {
			consumedField, ok, err := findJSONTypeField(consumedFields, producedField.name)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if producedField.stringQuote != consumedField.stringQuote {
				return fmt.Errorf("JSON string option is not guaranteed compatible for field %q", producedField.name)
			}
			if err := valueDependentCoercionSeen(producedField.typeOf, consumedField.typeOf, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if consumed.Kind() == reflect.Map {
		if !jsonMapKeyCanUnmarshal(consumed.Key()) || !jsonMapKeysCompatible(reflect.TypeFor[string](), consumed.Key()) {
			return fmt.Errorf("JSON map key values are not guaranteed compatible with %s", consumed.Key())
		}
		for _, producedField := range producedFields {
			if producedField.stringQuote {
				return fmt.Errorf("JSON string option on %s is not guaranteed compatible with map values", produced)
			}
			if err := valueDependentCoercionSeen(producedField.typeOf, consumed.Elem(), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func inspectMapCoercion(produced, consumed reflect.Type, seen map[coercionTypePair]struct{}) error {
	if !jsonMapKeyCanMarshal(produced.Key()) {
		return fmt.Errorf("JSON cannot marshal map key type %s", produced.Key())
	}
	if consumed.Kind() == reflect.Struct {
		consumedFields, err := jsonTypeFields(consumed)
		if err != nil {
			return err
		}
		for _, consumedField := range consumedFields {
			if consumedField.stringQuote {
				return fmt.Errorf("JSON string option on %s is not guaranteed compatible with map values", consumed)
			}
			if err := valueDependentCoercionSeen(produced.Elem(), consumedField.typeOf, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if consumed.Kind() == reflect.Map {
		if !jsonMapKeyCanUnmarshal(consumed.Key()) || !jsonMapKeysCompatible(produced.Key(), consumed.Key()) {
			return fmt.Errorf("JSON map key values are not guaranteed compatible from %s to %s", produced.Key(), consumed.Key())
		}
		return valueDependentCoercionSeen(produced.Elem(), consumed.Elem(), seen)
	}
	return nil
}

func jsonMapKeyCanMarshal(keyType reflect.Type) bool {
	return !keyType.Implements(reflect.TypeFor[encoding.TextMarshaler]()) &&
		(keyType.Kind() == reflect.String || isIntegerKind(keyType.Kind()))
}

func jsonMapKeyCanUnmarshal(keyType reflect.Type) bool {
	return !keyType.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) &&
		(keyType.Kind() == reflect.String || isIntegerKind(keyType.Kind())) &&
		(keyType.Kind() == reflect.Pointer || !reflect.PointerTo(keyType).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()))
}

func jsonMapKeysCompatible(produced, consumed reflect.Type) bool {
	if produced.Kind() == reflect.String {
		return consumed.Kind() == reflect.String
	}
	if !isIntegerKind(produced.Kind()) {
		return false
	}
	if consumed.Kind() == reflect.String {
		return true
	}
	return isIntegerKind(consumed.Kind()) && numericCoercionIsSafe(produced, consumed)
}

func isIntegerKind(kind reflect.Kind) bool {
	//nolint:exhaustive // only integer map key kinds are accepted
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return false
	}
}

func inspectSequenceCoercion(produced, consumed reflect.Type, seen map[coercionTypePair]struct{}) error {
	if consumed.Kind() == reflect.Array || consumed.Kind() == reflect.Slice {
		return valueDependentCoercionSeen(produced.Elem(), consumed.Elem(), seen)
	}
	return nil
}

func jsonTypeFields(structType reflect.Type) ([]jsonTypeField, error) {
	candidates := make([]jsonTypeField, 0, structType.NumField())
	if err := appendJSONTypeFields(structType, 0, &candidates, make(map[reflect.Type]bool)); err != nil {
		return nil, err
	}
	groups := make(map[string]jsonTypeFieldGroup, len(candidates))
	for _, candidate := range candidates {
		group, ok := groups[candidate.name]
		if !ok || candidate.depth < group.depth {
			group = jsonTypeFieldGroup{depth: candidate.depth}
		}
		if candidate.depth == group.depth {
			if candidate.tagged {
				group.tagged = candidate
				group.taggedCount++
			} else {
				group.untagged = candidate
				group.untaggedCount++
			}
		}
		groups[candidate.name] = group
	}

	fields := make([]jsonTypeField, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.name]; ok {
			continue
		}
		seen[candidate.name] = struct{}{}
		group := groups[candidate.name]
		switch {
		case group.taggedCount == 1:
			fields = append(fields, group.tagged)
		case group.taggedCount == 0 && group.untaggedCount == 1:
			fields = append(fields, group.untagged)
		}
	}
	return fields, nil
}

func appendJSONTypeFields(structType reflect.Type, depth int, fields *[]jsonTypeField, seen map[reflect.Type]bool) error {
	if seen[structType] {
		return nil
	}
	seen[structType] = true
	defer delete(seen, structType)

	for field := range structType.Fields() {
		candidate, include, err := jsonTypeFieldFor(field, depth)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		if field.Anonymous && !candidate.tagged {
			if embeddedType := jsonEmbeddedStructType(field.Type); embeddedType != nil {
				if err := appendJSONTypeFields(embeddedType, depth+1, fields, seen); err != nil {
					return err
				}
				continue
			}
		}
		if !field.IsExported() && (!field.Anonymous || jsonEmbeddedStructType(field.Type) == nil) {
			continue
		}
		*fields = append(*fields, candidate)
	}
	return nil
}

func jsonTypeFieldFor(field reflect.StructField, depth int) (jsonTypeField, bool, error) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return jsonTypeField{}, false, nil
	}
	name, options, _ := strings.Cut(tag, ",")
	if strings.ContainsAny(name, "\\'\"`") {
		return jsonTypeField{}, false, fmt.Errorf("JSON tag on field %s uses unsupported quoted or escaped name syntax", field.Name)
	}
	tagged := name != ""
	if !tagged {
		name = field.Name
	}
	var caseStrict, stringQuote bool
	for option := range strings.SplitSeq(options, ",") {
		switch option {
		case "case:strict":
			caseStrict = true
		case "case:ignore":
			caseStrict = false
		case jsonTagStringOption:
			stringQuote = true
		case "embed":
			return jsonTypeField{}, false, fmt.Errorf("JSON embed tag option on field %s is not modeled safely", field.Name)
		default:
			if strings.HasPrefix(option, "format:") || strings.HasPrefix(option, "case:") {
				return jsonTypeField{}, false, fmt.Errorf("JSON tag option %q on field %s is not modeled safely", option, field.Name)
			}
		}
	}
	return jsonTypeField{
		name:        name,
		typeOf:      field.Type,
		depth:       depth,
		tagged:      tagged,
		caseStrict:  caseStrict,
		stringQuote: stringQuote,
	}, true, nil
}

func jsonEmbeddedStructType(valueType reflect.Type) reflect.Type {
	if valueType.Kind() == reflect.Pointer && valueType.Name() == "" {
		valueType = valueType.Elem()
	}
	if valueType.Kind() != reflect.Struct {
		return nil
	}
	return valueType
}

func findJSONTypeField(fields []jsonTypeField, name string) (jsonTypeField, bool, error) {
	for _, field := range fields {
		if field.name == name {
			return field, true, nil
		}
	}
	var match jsonTypeField
	found := false
	for _, field := range fields {
		if field.caseStrict || !strings.EqualFold(field.name, name) {
			continue
		}
		if found {
			return jsonTypeField{}, false, fmt.Errorf("JSON field name %q matches multiple input fields", name)
		}
		match = field
		found = true
	}
	return match, found, nil
}

func isNumericKind(kind reflect.Kind) bool {
	//nolint:exhaustive // only numeric kinds are accepted
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func numericCoercionIsSafe(produced, consumed reflect.Type) bool {
	producedKind, consumedKind := produced.Kind(), consumed.Kind()
	producedSigned := producedKind >= reflect.Int && producedKind <= reflect.Int64
	consumedSigned := consumedKind >= reflect.Int && consumedKind <= reflect.Int64
	producedUnsigned := producedKind >= reflect.Uint && producedKind <= reflect.Uintptr
	consumedUnsigned := consumedKind >= reflect.Uint && consumedKind <= reflect.Uintptr
	producedFloat := producedKind == reflect.Float32 || producedKind == reflect.Float64
	consumedFloat := consumedKind == reflect.Float32 || consumedKind == reflect.Float64

	switch {
	case producedSigned && consumedSigned, producedUnsigned && consumedUnsigned:
		return consumed.Bits() >= produced.Bits()
	case producedUnsigned && consumedSigned:
		return consumed.Bits() > produced.Bits()
	case producedSigned && consumedFloat, producedUnsigned && consumedFloat:
		precision := 24
		if consumedKind == reflect.Float64 {
			precision = 53
		}
		integerBits := produced.Bits()
		if producedSigned {
			integerBits--
		}
		return integerBits <= precision
	case producedFloat && consumedFloat:
		// JSON cannot represent NaN or infinities.
		return false
	default:
		return false
	}
}

// coerceCheck probes the declared JSON shape with zero values. It is not a
// guarantee that arbitrary runtime values can be marshaled or decoded.
func coerceCheck(producerType, consumerType reflect.Type) error {
	producerValue := reflect.New(producerType).Elem().Interface()
	consumerPtr := reflect.New(consumerType)

	raw, err := json.Marshal(producerValue)
	if err != nil {
		return fmt.Errorf("cannot marshal %s: %w", producerType, err)
	}
	if err := json.Unmarshal(raw, consumerPtr.Interface()); err != nil {
		return fmt.Errorf("cannot coerce %s into %s: %w", producerType, consumerType, err)
	}
	return nil
}

var _ ContractProvider = (*DynamicResolver)(nil)

func (r *DynamicResolver) Contract(name string) (CapabilityContract, bool) {
	r.mu.RLock()
	contract, found := r.contracts[name]
	r.mu.RUnlock()
	if found {
		return contract, true
	}
	if act, ok := r.Action(name); ok {
		return actionContract(name, act), true
	}
	if stream, ok := r.Stream(name); ok {
		return CapabilityContract{
			Name:   name,
			Input:  TypeDescriptor{Type: payloadType(stream.ReqPayload())},
			Output: TypeDescriptor{Type: payloadType(stream.ResPayload()), Stream: true},
		}, true
	}
	if op, ok := r.Operator(name); ok {
		return CapabilityContract{
			Name:   name,
			Input:  TypeDescriptor{Type: op.InType, Stream: true},
			Output: TypeDescriptor{Type: op.OutType, Stream: true},
		}, true
	}
	return CapabilityContract{}, false
}

func (r *DynamicResolver) RegisterContract(name string, contract CapabilityContract) error {
	contract.Name = name
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actions[name]; !ok {
		return xerr.Validation(fmt.Sprintf("cannot register a type contract for unmounted action %q", name))
	}
	if current, ok := r.contracts[name]; ok && !reflect.DeepEqual(current, contract) {
		return xerr.Validation(fmt.Sprintf("conflicting type contract for action %q", name))
	}
	r.contracts[name] = contract
	return nil
}
