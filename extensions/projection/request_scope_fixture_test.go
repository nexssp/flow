package projection_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type requestScopeStage struct {
	Entry string
}

func TestProjectionInputRemainsOriginalRequestAfterTypedStage(t *testing.T) {
	stage := action.New("requestscope.entry", func(context.Context, any) (requestScopeStage, error) {
		return requestScopeStage{Entry: "current-stage"}, nil
	}).Build()
	cfg, err := runner.BuildConfig(append(native.Bundles(), compiler.Bundle{
		ID: "projection-request-scope-test",
		Libraries: []action.Library{{
			Name:    "requestscope",
			Actions: []action.AnyAction{stage},
		}},
	}))
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	fixture, err := fs.ReadFile(projectionext.Bundle(nil).Fixtures, "nflows/request_scope_after_typed_stage.nflow")
	if err != nil {
		t.Fatalf("read request-scope fixture: %v", err)
	}
	source := strings.ReplaceAll(string(fixture), "typedprojection.entry", "requestscope.entry")
	if source == string(fixture) {
		t.Fatal("test source did not contain the expected typed stage action")
	}
	if _, err := runner.Compile(t.Context(), cfg, source, "request_scope_after_typed_stage.nflow"); err != nil {
		t.Fatalf("compile request-scope fixture: %v", err)
	}
	result, err := runner.Execute(t.Context(), cfg, source, "request_scope_after_typed_stage.nflow", map[string]any{"lang": "en"})
	if err != nil {
		t.Fatalf("execute request-scope fixture: %v", err)
	}
	values, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", result.Output, result.Output)
	}
	if values["request_lang"] != "en" || values["stage_entry"] != "current-stage" {
		t.Fatalf("projected output = %#v, want request_lang=en and stage_entry=current-stage", values)
	}
}
