package syntax

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

func TestNamedParallel_FirstErrorUsesSourceOrder(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	parser := compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(bundle.Primaries...),
		`parallel { first: test.fail_first, second: test.fail_second }`,
	)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse parallel expression: %v", err)
	}

	secondFinished := make(chan struct{})
	first := action.New("test.fail_first", func(context.Context, any) (any, error) {
		<-secondFinished
		return nil, errors.New("first failure")
	}).Build()
	second := action.New("test.fail_second", func(context.Context, any) (any, error) {
		close(secondFinished)
		return nil, errors.New("second failure")
	}).Build()
	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	if err := resolver.Mount(action.Library{
		Name:    "parallel-test",
		Actions: []action.AnyAction{first, second},
	}); err != nil {
		t.Fatalf("mount test actions: %v", err)
	}
	if err := expr.Analyze(resolver); err != nil {
		t.Fatalf("analyze parallel expression: %v", err)
	}
	program, err := compiler.Build(context.Background(), resolver, nil, expr)
	if err != nil {
		t.Fatalf("build parallel expression: %v", err)
	}
	_, err = action.InvokeAny(context.Background(), program, nil)
	if err == nil {
		t.Fatal("parallel invocation succeeded; want branch error")
	}
	message := err.Error()
	if !strings.Contains(message, "parallel branch [first] failed") || !strings.Contains(message, "first failure") {
		t.Fatalf("parallel error = %v, want the first source branch's error", err)
	}
	if strings.Contains(message, "parallel branch [second] failed") {
		t.Fatalf("parallel error = %v, want first branch's error rather than second branch's error", err)
	}
}
