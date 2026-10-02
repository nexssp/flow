package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestParallelExpr_DuplicateActionNamesHaveDeterministicGatherKeys(t *testing.T) {
	ctx := context.Background()
	resolver := mustResolver(t, action.Library{
		Name: "parallel-test",
		Actions: []action.AnyAction{
			action.New("test.echo", func(_ context.Context, input map[string]any) (any, error) {
				return input["value"], nil
			}).Build(),
			action.New("test.keep", func(_ context.Context, input map[string]any) (any, error) {
				return input["value"], nil
			}).Build(),
		},
	})

	ast, err := NewParserWithPrimaries(
		ctx,
		testTable(),
		DefaultPrimaryExtensions(),
		`( test.echo @{ value: "first" } & test.keep @{ value: "solo" } & test.echo @{ value: "second" } & test.echo @{ value: "third" } )`,
	).Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	program, err := Build(ctx, resolver, nil, ast)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := map[string]any{
		"test.echo#1": "first",
		"test.keep":   "solo",
		"test.echo#2": "second",
		"test.echo#3": "third",
	}
	for run := range 20 {
		output, invokeErr := action.InvokeAny(ctx, program, nil)
		if invokeErr != nil {
			t.Fatalf("InvokeAny run %d: %v", run+1, invokeErr)
		}
		got, ok := output.(map[string]any)
		if !ok {
			t.Fatalf("output type = %T, want map[string]any", output)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d output = %#v, want %#v", run+1, got, want)
		}
	}
}
