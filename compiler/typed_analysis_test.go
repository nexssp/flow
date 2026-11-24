package compiler

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeTypedAtomValues(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}

	t.Run("nil expression checker", func(t *testing.T) {
		atom := &Atom{
			Name:        "test.unregistered",
			DirectInput: &Value{Kind: ValueExpression, Expression: ".name"},
		}
		if _, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver}, atom); err != nil {
			t.Fatalf("analyze atom without expression checker: %v", err)
		}
	})

	t.Run("direct input on unregistered atom", func(t *testing.T) {
		calls := 0
		ctx := TypeAnalysisContext{
			Resolver: resolver,
			CheckExpression: func(source string, _ TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
				calls++
				if source != ".name" {
					t.Errorf("checked expression = %q, want %q", source, ".name")
				}
				return TypeDescriptor{}, nil
			},
		}
		atom := &Atom{
			Name:        "test.unregistered",
			DirectInput: &Value{Kind: ValueExpression, Expression: ".name"},
		}
		if _, err := AnalyzeTypedExpression(ctx, atom); err != nil {
			t.Fatalf("analyze direct-input expression: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expression checker calls = %d, want 1", calls)
		}
	})
}

func TestAnalyzeTypedValueExprRecursesIntoArrayValues(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	calls := 0
	ctx := TypeAnalysisContext{
		Resolver: resolver,
		CheckExpression: func(source string, _ TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
			calls++
			if source != "name" {
				t.Errorf("checked expression = %q, want %q", source, "name")
			}
			return TypeDescriptor{Type: reflect.TypeFor[string]()}, nil
		},
	}
	expr := &ValueExpr{Value: &Value{Kind: ValueSlice, Slice: []*Value{
		{Kind: ValueRef, Ref: "name"},
		{Kind: ValueString, Str: "fallback"},
	}}}
	descriptor, err := AnalyzeTypedExpression(ctx, expr)
	if err != nil {
		t.Fatalf("analyze direct array: %v", err)
	}
	if descriptor.Type != reflect.TypeFor[[]string]() {
		t.Fatalf("array type = %v, want []string", descriptor.Type)
	}
	if calls != 1 {
		t.Fatalf("expression checker calls = %d, want 1", calls)
	}
}

func TestAnalyzeTypedParallelIsolatesBranchBindings(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	ctx := TypeAnalysisContext{
		Resolver: resolver,
		Bindings: map[string]TypeDescriptor{"outer": {Type: reflect.TypeFor[string]()}},
		CheckExpression: func(source string, expressionContext TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
			descriptor, ok := expressionContext.Bindings[source]
			if !ok {
				return TypeDescriptor{}, errors.New("unknown binding: " + source)
			}
			return descriptor, nil
		},
	}
	parallel := &ParallelExpr{Branches: []Expr{
		&BindingExpr{Name: "branchLocal", Inner: &LiteralExpr{Val: "value"}},
		&ValueExpr{Value: &Value{Kind: ValueRef, Ref: "branchLocal"}},
	}}
	if _, err := AnalyzeTypedExpression(ctx, parallel); err == nil {
		t.Fatal("sibling branch unexpectedly resolved another branch's binding")
	}
	parallel.Branches[1] = &ValueExpr{Value: &Value{Kind: ValueRef, Ref: "outer"}}
	if _, err := AnalyzeTypedExpression(ctx, parallel); err != nil {
		t.Fatalf("parallel branch could not resolve inherited outer binding: %v", err)
	}
	if _, ok := ctx.Bindings["branchLocal"]; ok {
		t.Fatal("branch-local binding leaked into parent context")
	}
}

func TestAnalyzeTypedSharedValueUsesCurrentContext(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	sharedValue := &Value{Kind: ValueExpression, Expression: "same"}
	checkerCalls := 0
	check := func(source string, ctx TypeAnalysisContext, pos Position) (TypeDescriptor, error) {
		checkerCalls++
		if source != "same" {
			t.Errorf("checked expression = %q, want %q", source, "same")
		}
		if ctx.Input.Type == reflect.TypeFor[bool]() {
			if pos.File != "second.flow" {
				t.Errorf("second analysis position = %q, want second.flow", pos.File)
			}
			return TypeDescriptor{}, errors.New("context-specific failure")
		}
		return TypeDescriptor{Type: reflect.TypeFor[string]()}, nil
	}
	first := &ValueExpr{Value: sharedValue, Pos: Position{File: "first.flow"}}
	if descriptor, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver, Input: TypeDescriptor{Type: reflect.TypeFor[string]()}, CheckExpression: check}, first); err != nil {
		t.Fatalf("first shared-value analysis: %v", err)
	} else if descriptor.Type != reflect.TypeFor[string]() {
		t.Fatalf("first descriptor type = %v, want string", descriptor.Type)
	}
	second := &ValueExpr{Value: sharedValue, Pos: Position{File: "second.flow"}}
	if _, err := AnalyzeTypedExpression(TypeAnalysisContext{Resolver: resolver, Input: TypeDescriptor{Type: reflect.TypeFor[bool]()}, CheckExpression: check}, second); err == nil || err.Error() != "context-specific failure" {
		t.Fatalf("second shared-value analysis error = %v, want context-specific failure", err)
	}
	if checkerCalls != 2 {
		t.Fatalf("expression checker calls = %d, want 2", checkerCalls)
	}
}

func TestAnalyzeTypedAssertChecksMessageExpression(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	var checked []string
	ctx := TypeAnalysisContext{
		Resolver: resolver,
		CheckExpression: func(source string, _ TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
			checked = append(checked, source)
			if source == "condition" {
				return TypeDescriptor{Type: reflect.TypeFor[bool]()}, nil
			}
			return TypeDescriptor{Type: reflect.TypeFor[string]()}, nil
		},
	}
	assertion := &AssertExpr{Condition: "condition", MessageExpression: "message"}
	if _, err := AnalyzeTypedExpression(ctx, assertion); err != nil {
		t.Fatalf("analyze assertion: %v", err)
	}
	if !reflect.DeepEqual(checked, []string{"condition", "message"}) {
		t.Fatalf("checked expressions = %v, want condition then message", checked)
	}
}

func TestAnalyzeTypedAssertRejectsKnownNonStringMessage(t *testing.T) {
	resolver, err := NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	ctx := TypeAnalysisContext{
		Resolver: resolver,
		CheckExpression: func(source string, _ TypeAnalysisContext, _ Position) (TypeDescriptor, error) {
			if source == "condition" {
				return TypeDescriptor{Type: reflect.TypeFor[bool]()}, nil
			}
			return TypeDescriptor{Type: reflect.TypeFor[int]()}, nil
		},
	}
	assertion := &AssertExpr{Condition: "condition", MessageExpression: "message"}
	if _, err := AnalyzeTypedExpression(ctx, assertion); err == nil || !strings.Contains(err.Error(), "message expression must return string") {
		t.Fatalf("analysis error = %v, want non-string message validation", err)
	}
}
