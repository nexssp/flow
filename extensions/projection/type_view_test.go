package projection

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/expr-lang/expr"

	"github.com/nexssp/flow/compiler"
)

type promotedProjectionFields struct {
	Name string
	ID   string
}

type directProjectionFields struct {
	promotedProjectionFields
	Name string
}

type valueMethodProjection struct {
	Name string
}

type pointerMethodProjection struct {
	Name string
}

func (valueMethodProjection) ValueName() string      { return "value" }
func (*pointerMethodProjection) PointerName() string { return "pointer" }

type recursiveProjectionNode struct {
	Name string
	Next *recursiveProjectionNode
}

func TestExpressionViewDirectFieldsShadowPromotedFields(t *testing.T) {
	value := directProjectionFields{
		promotedProjectionFields: promotedProjectionFields{Name: "embedded", ID: "id-1"},
		Name:                     "direct",
	}
	view, ok := expressionValueView(value).(map[string]any)
	if !ok {
		t.Fatalf("view = %T, want map[string]any", expressionValueView(value))
	}
	if view["name"] != "direct" || view["Name"] != "direct" {
		t.Fatalf("direct field aliases = %#v, want direct value to shadow promoted value", view)
	}
	if view["id"] != "id-1" {
		t.Fatalf("promoted non-conflicting field = %#v, want id-1", view["id"])
	}
}

func TestExpressionMapViewAliasCollisionsAreDeterministicAndNilIsPreserved(t *testing.T) {
	input := map[string]int{"HttpServer": 2, "HTTPServer": 1}
	for range 20 {
		view, ok := expressionValueView(input).(map[string]any)
		if !ok {
			t.Fatalf("map view = %T, want map[string]any", expressionValueView(input))
		}
		if view["http_server"] != 1 {
			t.Fatalf("collision alias = %#v, want lexically first source key HTTPServer", view["http_server"])
		}
	}
	var nilMap map[string]any
	view, ok := expressionValueView(nilMap).(map[string]any)
	if !ok || view != nil {
		t.Fatalf("nil map view = (%T, %#v), want a nil map", expressionValueView(nilMap), expressionValueView(nilMap))
	}
}

func TestProjectionStaticMethodSetsMatchRuntimeReceivers(t *testing.T) {
	valueType := reflect.TypeFor[valueMethodProjection]()
	value, ok := expressionValueView(valueMethodProjection{Name: "x"}).(map[string]any)
	if !ok {
		t.Fatalf("value view = %T, want map[string]any", expressionValueView(valueMethodProjection{Name: "x"}))
	}
	if _, found := value["ValueName"]; !found {
		t.Fatal("value receiver method missing from runtime value view")
	}
	pointerValue, ok := expressionValueView(&valueMethodProjection{Name: "x"}).(map[string]any)
	if !ok {
		t.Fatalf("pointer-to-value-method view = %T, want map[string]any", expressionValueView(&valueMethodProjection{Name: "x"}))
	}
	if _, found := pointerValue["ValueName"]; !found {
		t.Fatal("value receiver method missing from runtime pointer view")
	}
	pointerReceiverType := reflect.TypeFor[pointerMethodProjection]()
	valuePointer, ok := expressionValueView(pointerMethodProjection{Name: "x"}).(map[string]any)
	if !ok {
		t.Fatalf("value with pointer method view = %T, want map[string]any", expressionValueView(pointerMethodProjection{Name: "x"}))
	}
	if _, found := valuePointer["PointerName"]; found {
		t.Fatal("pointer receiver method unexpectedly exposed on a value")
	}
	pointer, ok := expressionValueView(&pointerMethodProjection{Name: "x"}).(map[string]any)
	if !ok {
		t.Fatalf("pointer view = %T, want map[string]any", expressionValueView(&pointerMethodProjection{Name: "x"}))
	}
	if _, ok := pointer["PointerName"]; !ok {
		t.Fatal("pointer receiver method missing from runtime pointer view")
	}

	valueEnv := map[string]any{"item": projectionStaticValue(valueType)}
	if _, err := compiler.CompileExpression("item.ValueName()", expr.Env(valueEnv), expr.AllowUndefinedVariables()); err != nil {
		t.Fatalf("compile value receiver method on value: %v", err)
	}
	pointerValueEnv := map[string]any{"item": projectionStaticValue(reflect.PointerTo(valueType))}
	if _, err := compiler.CompileExpression("item.ValueName()", expr.Env(pointerValueEnv), expr.AllowUndefinedVariables()); err != nil {
		t.Fatalf("compile value receiver method on pointer: %v", err)
	}
	pointerMethodEnv := map[string]any{"item": projectionStaticValue(pointerReceiverType)}
	if _, err := compiler.CompileExpression("item.PointerName()", expr.Env(pointerMethodEnv), expr.AllowUndefinedVariables()); err == nil {
		t.Fatal("static value proxy accepted a pointer-only method")
	}
	pointerEnv := map[string]any{"item": projectionStaticValue(reflect.PointerTo(pointerReceiverType))}
	if _, err := compiler.CompileExpression("item.PointerName()", expr.Env(pointerEnv), expr.AllowUndefinedVariables()); err != nil {
		t.Fatalf("compile pointer receiver method on pointer: %v", err)
	}
}

func TestTypedProjectionAllowsSourceLocalSymbolNamespaceRoots(t *testing.T) {
	ctx := compiler.TypeAnalysisContext{
		Input: compiler.TypeDescriptor{Type: reflect.TypeFor[recursiveProjectionNode]()},
		SymbolNamespaces: map[string]compiler.SymbolNamespace{
			"local_config": {
				Name:    "local_config",
				Symbols: map[string]any{"enabled": true},
			},
		},
	}
	if _, err := CheckTypedSourceExpression("local_config.enabled == true", ctx, compiler.Position{}); err != nil {
		t.Fatalf("compile source-local namespace path with a known Go input: %v", err)
	}
}

func TestProjectionNullCoalescingAllowsMissingSyntheticFieldsOnly(t *testing.T) {
	metadataType := reflect.StructOf([]reflect.StructField{{
		Name: "Field0",
		Type: reflect.TypeFor[string](),
		Tag:  reflect.StructTag(`expr:"source" flow_projection:"true"`),
	}})
	inputType := reflect.StructOf([]reflect.StructField{{
		Name: "Field0",
		Type: metadataType,
		Tag:  reflect.StructTag(`expr:"metadata" flow_projection:"true"`),
	}})
	projectionContext := compiler.TypeAnalysisContext{Input: compiler.TypeDescriptor{Type: inputType}}
	if _, err := CheckTypedSourceExpression(`input.metadata.ip_address ?? "0.0.0.0"`, projectionContext, compiler.Position{}); err != nil {
		t.Fatalf("compile fallback for absent synthetic projection field: %v", err)
	}
	if _, err := CheckTypedSourceExpression(`.app_env ?? "production"`, projectionContext, compiler.Position{}); err != nil {
		t.Fatalf("compile fallback for absent synthetic shorthand field: %v", err)
	}
	if _, err := CheckTypedSourceExpression("input.metadata.ip_address", projectionContext, compiler.Position{}); err == nil {
		t.Fatal("projection schema accepted a missing field without a null-coalescing fallback")
	}

	goContext := compiler.TypeAnalysisContext{Input: compiler.TypeDescriptor{Type: reflect.TypeFor[recursiveProjectionNode]()}}
	if _, err := CheckTypedSourceExpression(`input.missing ?? "fallback"`, goContext, compiler.Position{}); err == nil {
		t.Fatal("typed Go struct accepted a nonexistent field under null coalescing")
	}
}

func TestDynamicExpressionTypeErrorsAreNarrowlyClassified(t *testing.T) {
	cases := []struct {
		message string
		want    bool
	}{
		{message: `type nil has no field "value" (1:4)`, want: true},
		{message: `invalid operation: >= (mismatched types unknown and int) (1:19)`, want: true},
		{message: `unknown function "X": mismatched arity`, want: false},
		{message: `invalid operation: + (mismatched types string and int)`, want: false},
		{message: `unknown field value`, want: false},
	}
	for _, tc := range cases {
		if got := isDynamicExpressionTypeError(errors.New(tc.message)); got != tc.want {
			t.Errorf("isDynamicExpressionTypeError(%q) = %t, want %t", tc.message, got, tc.want)
		}
	}
}

type projectionBenchmarkNested struct {
	RequestID string
}

type projectionBenchmarkResult struct {
	Stdout   string
	ExitCode int
	OK       bool
	Nested   projectionBenchmarkNested
	Items    []projectionBenchmarkNested
	Dynamic  any
}

func BenchmarkExpressionValueViewTypedProjectionOutput(b *testing.B) {
	result := projectionBenchmarkResult{
		Stdout:   "typed result",
		ExitCode: 0,
		OK:       true,
		Nested:   projectionBenchmarkNested{RequestID: "request-42"},
		Items:    []projectionBenchmarkNested{{RequestID: "request-42"}},
		Dynamic:  map[string]any{"runtime_field": "available"},
	}
	cache := newExpressionViewTypeCache()
	if expressionValueViewWithCache(result, cache) == nil {
		b.Fatal("typed projection result view is nil")
	}
	b.ReportAllocs()
	var view any
	b.ResetTimer()
	for range b.N {
		view = expressionValueViewWithCache(result, cache)
	}
	if view == nil {
		b.Fatal("typed projection result view is nil")
	}
}

func BenchmarkExprEvaluatorTypedProjectionOutput(b *testing.B) {
	result := projectionBenchmarkResult{
		Stdout:   "typed result",
		ExitCode: 0,
		OK:       true,
		Nested:   projectionBenchmarkNested{RequestID: "request-42"},
		Items:    []projectionBenchmarkNested{{RequestID: "request-42"}},
		Dynamic:  map[string]any{"runtime_field": "available"},
	}
	evaluate := NewExprEvaluator()
	request := Request{
		Raw:   "stdout: .stdout, request_id: .nested.request_id, item_id: .items[0].request_id, runtime_field: .dynamic.runtime_field",
		Input: result,
	}
	if _, err := evaluate(context.Background(), request); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := evaluate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func TestExpressionViewTypeCacheBoundsAndFreshViews(t *testing.T) {
	result := projectionBenchmarkResult{
		Stdout:   "typed result",
		ExitCode: 0,
		OK:       true,
		Nested:   projectionBenchmarkNested{RequestID: "request-42"},
		Items:    []projectionBenchmarkNested{{RequestID: "request-42"}},
		Dynamic:  map[string]any{"runtime_field": "available"},
	}
	cache := newExpressionViewTypeCache()
	first, ok := expressionValueViewWithCache(result, cache).(map[string]any)
	if !ok {
		t.Fatal("first typed view is not a map")
	}
	result.Stdout = "updated"
	result.Nested.RequestID = "updated-id"
	second, ok := expressionValueViewWithCache(result, cache).(map[string]any)
	if !ok {
		t.Fatal("second typed view is not a map")
	}
	firstNested, ok := first["nested"].(map[string]any)
	if !ok {
		t.Fatal("first nested typed view is not a map")
	}
	secondNested, ok := second["nested"].(map[string]any)
	if !ok {
		t.Fatal("second nested typed view is not a map")
	}
	if first["stdout"] != "typed result" || firstNested["request_id"] != "request-42" {
		t.Fatalf("earlier view changed after a later conversion: %#v", first)
	}
	if second["stdout"] != "updated" || secondNested["request_id"] != "updated-id" {
		t.Fatalf("later view did not reflect the current input: %#v", second)
	}
	allocations := testing.AllocsPerRun(20, func() {
		_ = expressionValueViewWithCache(result, cache)
	})
	if allocations >= 40 {
		t.Fatalf("warm typed view allocated %.1f objects per call, want fewer than 40", allocations)
	}
	for index := range maxCachedExpressionViewTypes + 16 {
		valueType := reflect.StructOf([]reflect.StructField{{
			Name: "Field" + strconv.Itoa(index),
			Type: reflect.TypeFor[int](),
		}})
		cache.fieldsFor(valueType)
	}
	cache.mu.RLock()
	count := len(cache.fields)
	cache.mu.RUnlock()
	if count > maxCachedExpressionViewTypes {
		t.Fatalf("cached %d field types, want at most %d", count, maxCachedExpressionViewTypes)
	}
}

func TestExpressionViewTypeCacheConcurrentUse(t *testing.T) {
	cache := newExpressionViewTypeCache()
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 20 {
				result := projectionBenchmarkResult{Stdout: "typed result", Nested: projectionBenchmarkNested{RequestID: "request-42"}}
				view, ok := expressionValueViewWithCache(result, cache).(map[string]any)
				if !ok || view["stdout"] != "typed result" {
					t.Errorf("concurrent view = %#v, want lowercase stdout alias", view)
					return
				}
			}
		})
	}
	workers.Wait()
}

func TestRecursiveProjectionTypesStayCheckedToExpressionDepth(t *testing.T) {
	typ := reflect.TypeFor[recursiveProjectionNode]()
	env := map[string]any{"node": projectionStaticValue(typ, 16)}
	if _, err := compiler.CompileExpression("node.next.next.name", expr.Env(env), expr.AllowUndefinedVariables()); err != nil {
		t.Fatalf("compile known recursive field path: %v", err)
	}
	if _, err := compiler.CompileExpression("node.next.next.missing", expr.Env(env), expr.AllowUndefinedVariables()); err == nil {
		t.Fatal("static recursive proxy accepted an unknown field within its supported depth")
	}
	deepValidPath := "node" + strings.Repeat(".next", 12) + ".name"
	if _, err := compiler.CompileExpression(deepValidPath, expr.Env(env), expr.AllowUndefinedVariables()); err != nil {
		t.Fatalf("compile known recursive path beyond eight levels: %v", err)
	}
	deepMissingPath := "node" + strings.Repeat(".next", 12) + ".missing"
	if _, err := compiler.CompileExpression(deepMissingPath, expr.Env(env), expr.AllowUndefinedVariables()); err == nil {
		t.Fatal("static recursive proxy accepted an unknown field beyond eight levels")
	}
	deepPath := "node" + strings.Repeat(".next", 12) + ".missing"
	if err := validateTypedExpressionPaths(deepPath, compiler.Position{}, map[string]reflect.Type{"node": typ}, false); err == nil {
		t.Fatalf("AST validator accepted an unknown field after a deep recursive path: %s", deepPath)
	}
}

func TestExpressionViewConvertsDeepAndCyclicRecursiveValues(t *testing.T) {
	root := &recursiveProjectionNode{Name: "deep"}
	current := root
	for range 80 {
		current.Next = &recursiveProjectionNode{Name: "deep"}
		current = current.Next
	}
	deepPath := "node" + strings.Repeat(".next", 80) + ".name"
	typeContext := compiler.TypeAnalysisContext{Input: compiler.TypeDescriptor{Type: reflect.TypeFor[recursiveProjectionNode]()}}
	deepSource := "input" + strings.Repeat(".next", 80) + ".name"
	deepType, err := CheckTypedSourceExpression(deepSource, typeContext, compiler.Position{})
	if err != nil {
		t.Fatalf("typed checker rejected a valid path beyond its dynamic proxy boundary: %v", err)
	}
	if deepType.Type != reflect.TypeFor[string]() {
		t.Fatalf("deep control selector type = %v, want string", deepType.Type)
	}
	deepProjection, err := analyzeProjectionTypes("deep: "+deepSource, compiler.Position{}, typeContext, nil, nil, "")
	if err != nil {
		t.Fatalf("analyze deep projection output: %v", err)
	}
	deepFields, known := projectionSchemaFields(deepProjection.Type)
	if !known || len(deepFields) != 1 || deepFields[0].Tag.Get("expr") != "deep" || deepFields[0].Type != reflect.TypeFor[string]() {
		t.Fatalf("deep projection schema = (%v, %#v), want a string `deep` field", known, deepFields)
	}
	flowProjection, err := analyzeProjectionTypes(
		"value: childflow(ctx, input)",
		compiler.Position{},
		typeContext,
		nil,
		[]projectionBindingType{{
			name:   "childflow",
			typeOf: compiler.TypeDescriptor{Type: reflect.TypeFor[recursiveProjectionNode]()},
		}},
		"ctx",
	)
	if err != nil {
		t.Fatalf("analyze embedded Flow output schema: %v", err)
	}
	flowFields, known := projectionSchemaFields(flowProjection.Type)
	if !known || len(flowFields) != 1 || flowFields[0].Type != reflect.TypeFor[recursiveProjectionNode]() {
		t.Fatalf("embedded Flow output field type = (%v, %#v), want recursive Go output type", known, flowFields)
	}
	missingPath := "input" + strings.Repeat(".next", 80) + ".missing"
	if _, err := CheckTypedSourceExpression(missingPath, typeContext, compiler.Position{}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("typed checker did not reject a missing field beyond its dynamic proxy boundary: %v", err)
	}
	viewEnv := map[string]any{"node": expressionValueView(root)}
	program, err := compiler.CompileExpression(deepPath, expr.Env(viewEnv), expr.AllowUndefinedVariables())
	if err != nil {
		t.Fatalf("compile runtime recursive path deeper than the former limit: %v", err)
	}
	value, err := expr.Run(program, viewEnv)
	if err != nil || value != "deep" {
		t.Fatalf("evaluate deep recursive path = (%#v, %v), want deep", value, err)
	}

	cycle := &recursiveProjectionNode{Name: "cycle"}
	cycle.Next = cycle
	cycleEnv := map[string]any{"node": expressionValueView(cycle)}
	if _, err := CheckTypedSourceExpression("input.next.next.name", typeContext, compiler.Position{}); err != nil {
		t.Fatalf("typed checker rejected a valid cyclic-type path: %v", err)
	}
	cycleProgram, err := compiler.CompileExpression("node.next.next.name", expr.Env(cycleEnv), expr.AllowUndefinedVariables())
	if err != nil {
		t.Fatalf("compile recursive pointer-cycle path: %v", err)
	}
	value, err = expr.Run(cycleProgram, cycleEnv)
	if err != nil || value != "cycle" {
		t.Fatalf("evaluate recursive pointer-cycle path = (%#v, %v), want cycle", value, err)
	}

	cycleMap := map[string]any{}
	cycleMap["self"] = cycleMap
	mapView, ok := expressionValueView(cycleMap).(map[string]any)
	if !ok {
		t.Fatalf("cyclic map view = %T, want map[string]any", expressionValueView(cycleMap))
	}
	mapSelf, ok := mapView["self"].(map[string]any)
	if !ok || reflect.ValueOf(mapSelf).Pointer() != reflect.ValueOf(mapView).Pointer() {
		t.Fatal("cyclic map view did not preserve its self-reference")
	}

	cycleSlice := make([]any, 1)
	cycleSlice[0] = cycleSlice
	sliceView, ok := expressionValueView(cycleSlice).([]any)
	if !ok {
		t.Fatalf("cyclic slice view = %T, want []any", expressionValueView(cycleSlice))
	}
	sliceSelf, ok := sliceView[0].([]any)
	if !ok || reflect.ValueOf(sliceSelf).Pointer() != reflect.ValueOf(sliceView).Pointer() {
		t.Fatal("cyclic slice view did not preserve its self-reference")
	}
}
