package syntax

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

type ifNamedBool bool

func TestIfKeywordParsesElseIfInOrder(t *testing.T) {
	t.Parallel()

	parsed, err := newStructuredKeywordParser(
		`if (true) { first } else if (false) { second } else { third }`, false,
	).Parse()
	if err != nil {
		t.Fatalf("parse if expression: %v", err)
	}
	outer, ok := parsed.(*IfExpr)
	if !ok {
		t.Fatalf("parsed expression = %T, want *IfExpr", parsed)
	}
	inner, ok := outer.Else.(*IfExpr)
	if !ok {
		t.Fatalf("else branch = %T, want nested *IfExpr", outer.Else)
	}
	if outer.Condition != "true" || inner.Condition != "false" || inner.Else == nil {
		t.Fatalf("parsed else-if chain = %#v, want ordered conditions true then false and a final else", outer)
	}
}

func TestIfActionGraphDescribesTrueAndFalseRoutes(t *testing.T) {
	t.Parallel()

	expr := &IfExpr{
		Condition: "true",
		Then:      &compiler.Atom{Name: "then"},
		Else:      &compiler.Atom{Name: "else"},
	}
	graph, err := compiler.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatalf("build action graph: %v", err)
	}
	if len(graph.Nodes) == 0 || graph.Nodes[0].Kind != "if" {
		t.Fatalf("first graph node = %#v, want if choice", graph.Nodes)
	}
	if !hasEdge(graph, "true") || !hasEdge(graph, "false") {
		t.Fatalf("if graph is missing true/false routes: %#v", graph.Edges)
	}
}

func TestIfActionGraphDescribesOmittedElsePassThrough(t *testing.T) {
	t.Parallel()

	graph, err := compiler.BuildActionGraph(nil, &IfExpr{
		Condition: "false",
		Then:      &compiler.Atom{Name: "then"},
	})
	if err != nil {
		t.Fatalf("build action graph: %v", err)
	}
	if len(graph.Nodes) < 2 {
		t.Fatalf("graph nodes = %#v, want if gate and merge", graph.Nodes)
	}
	gate, merge := graph.Nodes[0].ID, graph.Nodes[1].ID
	for _, edge := range graph.Edges {
		if edge.From == gate && edge.To == merge && edge.Kind == "false" {
			return
		}
	}
	t.Fatalf("if graph is missing a direct false pass-through route: %#v", graph.Edges)
}

func TestIfActionGraphIncludesSourcePosition(t *testing.T) {
	t.Parallel()

	want := compiler.Position{File: "if-position.nflow", Line: 4, Col: 3}
	graph, err := compiler.BuildActionGraph(nil, &IfExpr{
		Condition: "true",
		Then:      &compiler.Atom{Name: "then"},
		Pos:       want,
	})
	if err != nil {
		t.Fatalf("build action graph: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("action graph has no if node")
	}
	got := graph.Nodes[0]
	if got.File != want.File || got.Line != want.Line || got.Column != want.Col {
		t.Fatalf("if node position = %s:%d:%d, want %s:%d:%d", got.File, got.Line, got.Column, want.File, want.Line, want.Col)
	}
}

func TestIfTypeAnalysisRetainsOnlyCommonReachableOutput(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	ctx := compiler.TypeAnalysisContext{
		Resolver: resolver,
		Input:    compiler.TypeDescriptor{Type: reflect.TypeFor[string]()},
		CheckExpression: func(string, compiler.TypeAnalysisContext, compiler.Position) (compiler.TypeDescriptor, error) {
			return compiler.TypeDescriptor{Type: reflect.TypeFor[bool]()}, nil
		},
	}

	for _, tc := range []struct {
		name string
		expr *IfExpr
		want reflect.Type
	}{
		{
			name: "equal explicit branches",
			expr: &IfExpr{Condition: "true", Then: &compiler.LiteralExpr{Val: "then"}, Else: &compiler.LiteralExpr{Val: "else"}},
			want: reflect.TypeFor[string](),
		},
		{
			name: "mismatched explicit branches",
			expr: &IfExpr{Condition: "true", Then: &compiler.LiteralExpr{Val: "then"}, Else: &compiler.LiteralExpr{Val: 1}},
		},
		{
			name: "pass-through agrees with true branch",
			expr: &IfExpr{Condition: "true", Then: &compiler.LiteralExpr{Val: "then"}},
			want: reflect.TypeFor[string](),
		},
		{
			name: "pass-through disagrees with true branch",
			expr: &IfExpr{Condition: "true", Then: &compiler.LiteralExpr{Val: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output, err := tc.expr.AnalyzeTypes(ctx)
			if err != nil {
				t.Fatalf("analyze if output: %v", err)
			}
			if output.Type != tc.want {
				t.Fatalf("inferred output type = %v, want %v", output.Type, tc.want)
			}
		})
	}
}

func TestIfTypeAnalysisRejectsKnownNonBooleanCondition(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	wantPos := compiler.Position{File: "if-type.nflow", Line: 7, Col: 9}
	for _, tc := range []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "integer", typeOf: reflect.TypeFor[int]()},
		{name: "defined bool", typeOf: reflect.TypeFor[ifNamedBool]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ifExpr := &IfExpr{
				Condition:    "condition",
				ConditionPos: wantPos,
				Then:         &compiler.LiteralExpr{Val: "then"},
				Else:         &compiler.LiteralExpr{Val: "else"},
			}
			_, err := ifExpr.AnalyzeTypes(compiler.TypeAnalysisContext{
				Resolver: resolver,
				CheckExpression: func(string, compiler.TypeAnalysisContext, compiler.Position) (compiler.TypeDescriptor, error) {
					return compiler.TypeDescriptor{Type: tc.typeOf}, nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "if: condition must be bool") {
				t.Fatalf("type-analysis error = %v, want non-boolean condition diagnostic", err)
			}
			if got := xerr.KindFrom(err); got == xerr.KindValidation {
				t.Fatalf("compile-time type error has runtime validation kind: %v", err)
			}
			if got, ok := compiler.PositionOf(err); !ok || got != wantPos {
				t.Fatalf("type-analysis position = %v, %t; want %v", got, ok, wantPos)
			}
		})
	}
}

func TestIfTypeAnalysisAllowsCanonicalBoolAndInterfaceConditions(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	for _, typeOf := range []reflect.Type{reflect.TypeFor[bool](), reflect.TypeFor[any]()} {
		t.Run(typeOf.String(), func(t *testing.T) {
			t.Parallel()
			ifExpr := &IfExpr{
				Condition: "condition",
				Then:      &compiler.LiteralExpr{Val: "then"},
			}
			_, err := ifExpr.AnalyzeTypes(compiler.TypeAnalysisContext{
				Resolver: resolver,
				CheckExpression: func(string, compiler.TypeAnalysisContext, compiler.Position) (compiler.TypeDescriptor, error) {
					return compiler.TypeDescriptor{Type: typeOf}, nil
				},
			})
			if err != nil {
				t.Fatalf("type analysis rejected %s condition: %v", typeOf, err)
			}
		})
	}
}

func TestIfTypeAnalysisPreservesBooleanPassThroughType(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	output, err := (&IfExpr{
		Condition: "condition",
		Then:      &compiler.LiteralExpr{Val: true},
	}).AnalyzeTypes(compiler.TypeAnalysisContext{
		Resolver: resolver,
		Input:    compiler.TypeDescriptor{Type: reflect.TypeFor[bool]()},
		CheckExpression: func(string, compiler.TypeAnalysisContext, compiler.Position) (compiler.TypeDescriptor, error) {
			return compiler.TypeDescriptor{Type: reflect.TypeFor[bool]()}, nil
		},
	})
	if err != nil {
		t.Fatalf("analyze boolean pass-through: %v", err)
	}
	if output.Type != reflect.TypeFor[bool]() {
		t.Fatalf("pass-through output type = %v, want bool", output.Type)
	}
}
