package projection

import (
	"context"
	"reflect"
	"testing"

	"github.com/expr-lang/expr"

	"github.com/nexssp/flow/compiler"
)

func TestProjectionRootViewSelectionMatchesFullViewForStaticPaths(t *testing.T) {
	ctx := context.Background()
	input := map[string]any{
		"config": map[string]any{"ProfileName": "demo", "other": []any{"nested", "data"}},
		"unused": &recursiveProjectionNode{Name: "not needed"},
	}
	const raw = "value: .config.profile_name"

	pipelineInputNames := compiler.ExpressionPipelineInputBindingNames(ctx)
	environment := compiler.MergeSymbolNamespaces(ctx, buildEnv(input))
	rootBindings := expressionRootMapVisibleBindings(input, pipelineInputNames)
	visibleNames := projectionRuntimeVisibleNames(ctx)
	for name := range environment {
		visibleNames[name] = struct{}{}
	}
	for name := range rootBindings {
		visibleNames[name] = struct{}{}
	}
	selection, safe := newProgramCache().rootViewSelection(raw, visibleNames, pipelineInputNames, rootBindings, input)
	if !safe {
		t.Fatal("static root path was not selected")
	}
	if len(selection.topLevelFields) != 1 {
		t.Fatalf("selected fields = %#v, want only config", selection.topLevelFields)
	}
	if _, selected := selection.topLevelFields["config"]; !selected {
		t.Fatalf("selected fields = %#v, want config", selection.topLevelFields)
	}

	partialEnvironment := normalizeExpressionEnvironmentForProjectionWithRootSelection(ctx, environment, newExpressionViewTypeCache(), raw, selection, pipelineInputNames)
	partialRoot, ok := partialEnvironment[expressionRootName].(map[string]any)
	if !ok {
		t.Fatalf("partial current root = %T, want map[string]any", partialEnvironment[expressionRootName])
	}
	if _, ok := partialRoot["unused"].(*recursiveProjectionNode); !ok {
		t.Fatalf("unselected root field was converted: %T", partialRoot["unused"])
	}
	if _, exists := input["config"].(map[string]any)["profile_name"]; exists {
		t.Fatal("selective conversion added aliases to the input map")
	}

	got, err := NewExprEvaluator()(ctx, Request{Raw: raw, Input: input})
	if err != nil {
		t.Fatalf("evaluate with selective root view: %v", err)
	}
	want, err := evaluateProjectionWithFullRootView(ctx, raw, input)
	if err != nil {
		t.Fatalf("evaluate with full root view: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selective result = %#v, full result = %#v", got, want)
	}
}

func TestProjectionRootViewSelectionPreservesExactKeyOverAlias(t *testing.T) {
	ctx := context.Background()
	input := map[string]any{
		"HTTPServer":  map[string]any{"profile_name": "alias"},
		"http_server": map[string]any{"profile_name": "exact"},
		"unused":      &recursiveProjectionNode{Name: "not needed"},
	}
	const raw = "value: .http_server.profile_name"

	pipelineInputNames := compiler.ExpressionPipelineInputBindingNames(ctx)
	environment := compiler.MergeSymbolNamespaces(ctx, buildEnv(input))
	rootBindings := expressionRootMapVisibleBindings(input, pipelineInputNames)
	visibleNames := projectionRuntimeVisibleNames(ctx)
	for name := range environment {
		visibleNames[name] = struct{}{}
	}
	for name := range rootBindings {
		visibleNames[name] = struct{}{}
	}
	selection, safe := newProgramCache().rootViewSelection(raw, visibleNames, pipelineInputNames, rootBindings, input)
	if !safe || len(selection.topLevelFields) != 1 {
		t.Fatalf("selection = %#v, safe = %t; want exact-key-only selection", selection, safe)
	}
	if _, selected := selection.topLevelFields["http_server"]; !selected {
		t.Fatalf("selected fields = %#v, want exact http_server key", selection.topLevelFields)
	}
	if _, selected := selection.topLevelFields["HTTPServer"]; selected {
		t.Fatalf("selected fields = %#v, alias collision should not select HTTPServer", selection.topLevelFields)
	}

	got, err := NewExprEvaluator()(ctx, Request{Raw: raw, Input: input})
	if err != nil {
		t.Fatalf("evaluate with selective root view: %v", err)
	}
	want, err := evaluateProjectionWithFullRootView(ctx, raw, input)
	if err != nil {
		t.Fatalf("evaluate with full root view: %v", err)
	}
	gotMap, ok := got.(map[string]any)
	if !reflect.DeepEqual(got, want) || !ok || gotMap["value"] != "exact" {
		t.Fatalf("selective result = %#v, full result = %#v; want exact-key value", got, want)
	}
}

func TestProjectionRootViewSelectionDoesNotStealProtectedPipelineBinding(t *testing.T) {
	input := map[string]any{
		"request": map[string]any{"current": "payload"},
		"unused":  &recursiveProjectionNode{Name: "not needed"},
	}
	ctx := compiler.WithExpressionScope(context.Background(), input)
	if err := compiler.BindPipelineInputValue(ctx, "request", map[string]any{"current": "typed"}); err != nil {
		t.Fatalf("bind typed pipeline input: %v", err)
	}
	const raw = "value: request.current"

	pipelineInputNames := compiler.ExpressionPipelineInputBindingNames(ctx)
	environment := compiler.MergeSymbolNamespaces(ctx, buildEnv(input))
	rootBindings := expressionRootMapVisibleBindings(input, pipelineInputNames)
	if _, exists := rootBindings["request"]; exists {
		t.Fatal("protected pipeline input was exposed as a current-root alias")
	}
	visibleNames := projectionRuntimeVisibleNames(ctx)
	for name := range environment {
		visibleNames[name] = struct{}{}
	}
	for name := range rootBindings {
		visibleNames[name] = struct{}{}
	}
	selection, safe := newProgramCache().rootViewSelection(raw, visibleNames, pipelineInputNames, rootBindings, input)
	if !safe || len(selection.topLevelFields) != 0 {
		t.Fatalf("selection = %#v, safe = %t; want no current-root fields", selection, safe)
	}

	got, err := NewExprEvaluator()(ctx, Request{Raw: raw, Input: input})
	if err != nil {
		t.Fatalf("evaluate with selective root view: %v", err)
	}
	want, err := evaluateProjectionWithFullRootView(ctx, raw, input)
	if err != nil {
		t.Fatalf("evaluate with full root view: %v", err)
	}
	gotMap, ok := got.(map[string]any)
	if !reflect.DeepEqual(got, want) || !ok || gotMap["value"] != "typed" {
		t.Fatalf("selective result = %#v, full result = %#v; want typed pipeline value", got, want)
	}
}

func TestProjectionRootViewSelectionFallsBackForUnsupportedUses(t *testing.T) {
	ctx := context.Background()
	input := map[string]any{
		"config":     map[string]any{"profile_name": "demo"},
		"items":      []any{map[string]any{"name": "item"}},
		"HTTPServer": map[string]any{"profile_name": "aliased"},
	}
	pipelineInputNames := compiler.ExpressionPipelineInputBindingNames(ctx)
	environment := compiler.MergeSymbolNamespaces(ctx, buildEnv(input))
	rootBindings := expressionRootMapVisibleBindings(input, pipelineInputNames)
	visibleNames := projectionRuntimeVisibleNames(ctx)
	for name := range environment {
		visibleNames[name] = struct{}{}
	}
	for name := range rootBindings {
		visibleNames[name] = struct{}{}
	}

	for _, raw := range []string{
		"value: __root__",
		"value: .items[0].name",
		"value: config.items[0].name",
		"..., added: 1",
		"value: .http_server.profile_name",
	} {
		t.Run(raw, func(t *testing.T) {
			if selection, safe := newProgramCache().rootViewSelection(raw, visibleNames, pipelineInputNames, rootBindings, input); safe || selection != nil {
				t.Fatalf("selection = %#v, safe = %t; want conservative full-view fallback", selection, safe)
			}
		})
	}
}

func TestProjectionRootViewSelectionFallsBackWhenSelectedValueCyclesToRoot(t *testing.T) {
	root := map[string]any{}
	root["selected"] = map[string]any{"owner": root}
	root["unused"] = map[string]any{"CamelField": "preserved"}
	selection := &expressionRootViewSelection{topLevelFields: map[string]struct{}{"selected": {}}}

	got, ok := expressionValueViewWithRootSelection(root, newExpressionViewTypeCache(), selection).(map[string]any)
	if !ok {
		t.Fatalf("selected root view = %T, want map[string]any", expressionValueViewWithRootSelection(root, newExpressionViewTypeCache(), selection))
	}
	want, ok := expressionValueViewWithCache(root, newExpressionViewTypeCache()).(map[string]any)
	if !ok {
		t.Fatal("full root view is not a map")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cyclic selected view differs from full view: got %#v, want %#v", got, want)
	}
	unused, ok := got["unused"].(map[string]any)
	if !ok || unused["camel_field"] != "preserved" {
		t.Fatalf("cycle fallback did not produce a fully converted unselected field: %#v", got["unused"])
	}
}

func evaluateProjectionWithFullRootView(ctx context.Context, raw string, input any) (any, error) {
	pipelineInputNames := compiler.ExpressionPipelineInputBindingNames(ctx)
	environment := compiler.MergeSymbolNamespaces(ctx, buildEnv(input))
	env := normalizeExpressionEnvironmentForProjection(ctx, environment, newExpressionViewTypeCache(), raw, pipelineInputNames)
	if err := rejectUnavailableProjectionBindings(raw, compiler.ExpressionUnavailableBindingNames(ctx), env); err != nil {
		return nil, err
	}
	visibleNames := projectionRuntimeVisibleNames(ctx)
	for name := range env {
		visibleNames[name] = struct{}{}
	}
	program, err := newProgramCache().compileScoped(raw, visibleNames, pipelineInputNames)
	if err != nil {
		return nil, err
	}
	if err := rejectRuntimeProjectionBindingConflict(ctx, raw, env[expressionRootName]); err != nil {
		return nil, err
	}
	return expr.Run(program, env)
}
