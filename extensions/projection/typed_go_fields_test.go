package projection_test

import (
	"context"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type untaggedProjectionNested struct {
	RequestID string
}

type untaggedProjectionResult struct {
	Stdout   string
	ExitCode int
	OK       bool
	Nested   untaggedProjectionNested
	Items    []untaggedProjectionNested
	Dynamic  any
}

func (r untaggedProjectionResult) Summary() string { return r.Stdout }

func TestTypedGoFieldsProjectLowercaseWithoutJSONTags(t *testing.T) {
	var calls atomic.Int32
	produce := action.New("typedprojection.produce", func(context.Context, any) (untaggedProjectionResult, error) {
		calls.Add(1)
		return untaggedProjectionResult{
			Stdout:   "typed result",
			ExitCode: 0,
			OK:       true,
			Nested:   untaggedProjectionNested{RequestID: "request-42"},
			Items:    []untaggedProjectionNested{{RequestID: "request-42"}},
			Dynamic:  map[string]any{"runtime_field": "available"},
		}, nil
	}).Build()
	different := action.New("typedprojection.different", func(context.Context, any) (string, error) {
		return "different output", nil
	}).Build()
	booleanCondition := action.New("typedprojection.boolean", func(context.Context, any) (bool, error) {
		return true, nil
	}).Build()
	dynamicCondition := action.New("typedprojection.dynamic", func(context.Context, any) (any, error) {
		return true, nil
	}).Build()

	bundles := append(native.Bundles(), compiler.Bundle{
		ID: "typed-projection-untagged-test",
		Libraries: []action.Library{{
			Name:    "typedprojection",
			Actions: []action.AnyAction{produce, different, booleanCondition, dynamicCondition},
		}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	fixture, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, "nflows/typed_go_fields.nflow")
	if err != nil {
		t.Fatalf("read typed field fixture: %v", err)
	}
	source := strings.ReplaceAll(string(fixture), "cov.typed_projection", "typedprojection.produce")

	invalid := strings.Replace(source, "execution.exit_code", "execution.missing_field", 1)
	if invalid == source {
		t.Fatal("test source did not contain the expected lowercase field access")
	}
	_, err = runner.Compile(t.Context(), cfg, invalid, "typed_go_fields_invalid.nflow")
	if err == nil || !strings.Contains(err.Error(), "missing_field") {
		t.Fatalf("compile invalid field = %v, want a compile-time missing_field error", err)
	}
	if pos, ok := compiler.PositionOf(err); !ok || pos.Line <= 0 {
		t.Fatalf("compile error position = (%+v, %v), want a source-positioned field error", pos, ok)
	}
	nestedInvalid := strings.Replace(source, "execution.nested.request_id", "execution.nested.missing_field", 1)
	if nestedInvalid == source {
		t.Fatal("test source did not contain the expected nested lowercase field access")
	}
	if _, err := runner.Compile(t.Context(), cfg, nestedInvalid, "typed_go_fields_nested_invalid.nflow"); err == nil || !strings.Contains(err.Error(), "missing_field") {
		t.Fatalf("compile invalid nested field = %v, want a compile-time missing_field error", err)
	}
	indexedInvalid := strings.Replace(source, "execution.items[0].request_id", "execution.items[0].missing_field", 1)
	if indexedInvalid == source {
		t.Fatal("test source did not contain the expected indexed typed field access")
	}
	if _, err := runner.Compile(t.Context(), cfg, indexedInvalid, "typed_go_fields_indexed_invalid.nflow"); err == nil || !strings.Contains(err.Error(), "missing_field") {
		t.Fatalf("compile invalid indexed field = %v, want a compile-time missing_field error", err)
	}
	invalidRoot := strings.Replace(source, "root_stdout: .stdout", "root_stdout: .missing_field", 1)
	if invalidRoot == source {
		t.Fatal("test source did not contain the expected typed root field access")
	}
	compileCases := []struct {
		name   string
		source string
	}{
		{"typed pipeline root", invalidRoot},
		{"conditional condition", `typedprojection.produce as execution -> { value: execution.missing_field } ? noop : noop`},
		{"loop body", `typedprojection.produce as execution -> loop({ value: execution.missing_field }) until(true)`},
		{"race candidate", `typedprojection.produce as execution -> race { { value: execution.missing_field } }`},
		{"race result propagation", `race { typedprojection.produce } as execution -> { value: execution.missing_field }`},
		{"loop result propagation", `loop(typedprojection.produce) until(true) as execution -> { value: execution.missing_field }`},
		{"named parallel branch", `typedprojection.produce as execution -> parallel { branch: { value: execution.missing_field } }`},
		{"named parallel result key", `parallel { branch: typedprojection.produce } as result -> { value: result.missing_field }`},
		{"named parallel nested result field", `parallel { branch: typedprojection.produce } as result -> { value: result.branch.missing_field }`},
		{"match branch", `typedprojection.produce as execution -> match { _ -> { value: execution.missing_field } }`},
		{"match subject", `typedprojection.produce as execution -> match(execution.missing_field) { _ -> noop }`},
		{"match case predicate", `typedprojection.produce as execution -> match(execution.stdout) { execution.missing_field == "x" -> noop, _ -> noop }`},
		{"assert predicate", `typedprojection.produce as execution -> assert(execution.missing_field, "bad")`},
		{"loop until predicate", `loop({ value: 1 }) until(.missing_field)`},
		{"on_error handler input", `typedprojection.produce -> (typedprojection.different ? on_error { else -> { value: .missing_field } })`},
		{"error protected expression", `typedprojection.produce -> { value: .missing_field } ? on_error { else -> runtime.noop }`},
		{"Flow value nested in projection", `typedprojection.produce as execution -> { nested: race { { value: execution.missing_field } } }`},
		{"projection output key", `typedprojection.produce -> { known: 1 } as projected -> { value: projected.missing_field }`},
		{"nested projection output key", `typedprojection.produce -> { nested: { known: 1 } } as projected -> { value: projected.nested.missing_field }`},
		{"Flow value output schema", `typedprojection.produce -> { nested: race { typedprojection.produce } } as projected -> { value: projected.nested.missing_field }`},
		{"match branch after output mismatch", `typedprojection.produce as execution -> match { true -> typedprojection.produce, false -> typedprojection.different, _ -> { value: execution.missing_field } }`},
		{"race candidate after output mismatch", `typedprojection.produce as execution -> race { typedprojection.produce, typedprojection.different, { value: execution.missing_field } }`},
		{"on_error handler after output mismatch", `typedprojection.produce -> (typedprojection.different ? on_error { xerr.KindUnavailable -> typedprojection.produce, else -> { value: .missing_field } })`},
	}
	for _, tc := range compileCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runner.Compile(t.Context(), cfg, tc.source, "typed_go_fields_"+strings.ReplaceAll(tc.name, " ", "_")+".nflow"); err == nil || !strings.Contains(err.Error(), "missing_field") {
				t.Fatalf("compile invalid field = %v, want a compile-time missing_field error", err)
			}
		})
	}
	if _, err := runner.Compile(t.Context(), cfg, `parallel { branch: typedprojection.produce } as result -> { value: result.branch.stdout }`, "typed_go_fields_parallel_valid.nflow"); err != nil {
		t.Fatalf("compile known named-parallel branch field: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce -> { known: 1, nested: { request_id: "ok" }, items: [{ id: 3 }] } as projected -> { value: projected.known, request: projected.nested.request_id, item: projected.items[0].id }`, "typed_projection_result_schema.nflow"); err != nil {
		t.Fatalf("compile known projection output schema: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce -> { nested: race { typedprojection.produce } } as projected -> { value: projected.nested.stdout }`, "typed_projection_flow_value_schema.nflow"); err != nil {
		t.Fatalf("compile known embedded Flow output schema: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce as execution -> assert(execution.ok, "must be okay")`, "typed_assert_valid.nflow"); err != nil {
		t.Fatalf("compile boolean assertion: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce as execution -> assert(execution.dynamic, "dynamic assertion stays runtime-checked")`, "typed_assert_dynamic.nflow"); err != nil {
		t.Fatalf("compile dynamic assertion: %v, want dynamic interface values to remain runtime-checked", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.different ? noop : noop`, "typed_conditional_wrong_type.nflow"); err == nil || !strings.Contains(err.Error(), "conditional: condition must be bool") {
		t.Fatalf("compile string conditional condition = %v, want a static bool-type error", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.boolean ? noop : noop`, "typed_conditional_valid.nflow"); err != nil {
		t.Fatalf("compile boolean conditional condition: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.dynamic ? noop : noop`, "typed_conditional_dynamic.nflow"); err != nil {
		t.Fatalf("compile dynamic conditional condition: %v, want runtime-only interface checking", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce as execution -> assert(execution.stdout, "wrong static assertion type")`, "typed_assert_wrong_type.nflow"); err == nil || !strings.Contains(err.Error(), "condition must be bool") {
		t.Fatalf("compile string assertion condition = %v, want a static bool-type error", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `loop({ value: 1 }) until(.value == 1)`, "typed_loop_projection_valid.nflow"); err != nil {
		t.Fatalf("compile statically typed loop condition: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `loop({ value: 1 }) until(.value)`, "typed_loop_wrong_type.nflow"); err == nil || !strings.Contains(err.Error(), "until condition must be bool") {
		t.Fatalf("compile integer loop condition = %v, want a static bool-type error", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `loop(typedprojection.produce) until(.dynamic)`, "typed_loop_dynamic.nflow"); err != nil {
		t.Fatalf("compile dynamic loop condition: %v, want runtime-only interface checking", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `loop(typedprojection.produce) until(.ok)`, "typed_loop_valid.nflow"); err != nil {
		t.Fatalf("compile boolean loop condition: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce as execution -> match(execution.stdout) { "typed result" -> noop, _ -> noop }`, "typed_match_valid.nflow"); err != nil {
		t.Fatalf("compile typed match subject and condition: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, `typedprojection.produce as execution -> { value: execution.dynamic.runtime_field }`, "typed_go_fields_dynamic.nflow"); err != nil {
		t.Fatalf("compile dynamic interface field: %v, want unknown nested shape to remain permissive", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("invalid compile invoked typed action %d times", got)
	}
	schemaFixture, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, "nflows/static_output_schema.nflow")
	if err != nil {
		t.Fatalf("read static projection schema fixture: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, string(schemaFixture), "static_output_schema.nflow"); err != nil {
		t.Fatalf("compile static projection schema fixture: %v", err)
	}
	schemaResult, err := runner.Execute(t.Context(), cfg, string(schemaFixture), "static_output_schema.nflow", nil)
	if err != nil {
		t.Fatalf("execute static projection schema fixture: %v", err)
	}
	schemaValues, ok := schemaResult.Output.(map[string]any)
	if !ok || schemaValues["value"] != 1 || schemaValues["request_id"] != "request-42" || schemaValues["item_id"] != 7 {
		t.Fatalf("static projection fixture result = %#v", schemaResult.Output)
	}

	if _, err := runner.Compile(t.Context(), cfg, source, "typed_go_fields.nflow"); err != nil {
		t.Fatalf("compile lowercase untagged fields: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, source, "typed_go_fields.nflow", nil)
	if err != nil {
		t.Fatalf("execute lowercase untagged fields: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	if values["stdout"] != "typed result" || values["exit_code"] != 0 || values["ok"] != true || values["summary"] != "typed result" || values["root_stdout"] != "typed result" || values["nested_request_id"] != "request-42" || values["indexed_request_id"] != "request-42" || values["root_indexed_request_id"] != "request-42" {
		t.Fatalf("lowercase projected output = %#v", values)
	}
	nested, ok := values["nested"].(map[string]any)
	if !ok || nested["request_id"] != "request-42" {
		t.Fatalf("nested lowercase projection = %#v, want request_id=request-42", values["nested"])
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("typed action calls = %d, want exactly one runtime call", got)
	}
}
