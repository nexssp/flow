package compiler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
)

func TestBuildActionGraph_SequenceAndParallel(t *testing.T) {
	expr := &PipeExpr{
		L: &Atom{Name: "source"},
		R: &ParallelExpr{Branches: []Expr{
			&Atom{Name: "left"},
			&Atom{Name: "right"},
		}},
	}

	graph, err := BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Entry != "n1" || graph.Exit == "" {
		t.Fatalf("entry/exit = %q/%q, want a non-empty graph", graph.Entry, graph.Exit)
	}
	if len(graph.Nodes) != 5 {
		t.Fatalf("got %d nodes, want source, fork, join, and two branches", len(graph.Nodes))
	}
	if !hasGraphEdge(graph, "data") || !hasGraphEdge(graph, "branch 1") || !hasGraphEdge(graph, "result:branch 2") {
		t.Fatalf("graph is missing expected data/fan-out/join edges: %#v", graph.Edges)
	}
}

func TestBuildActionGraph_LoopHasControlCycle(t *testing.T) {
	graph, err := BuildActionGraph(nil, &LoopExpr{Body: &Atom{Name: "step"}, Until: ".done"})
	if err != nil {
		t.Fatal(err)
	}
	loopID := graph.Entry
	if !hasGraphEdgeFromTo(graph, "check-until", "n3", loopID) {
		t.Fatalf("loop graph lacks body-to-condition edge: %#v", graph.Edges)
	}
	if !hasGraphEdgeFromTo(graph, "repeat-if-false", loopID, "n3") {
		t.Fatalf("loop graph lacks the repeat back-edge: %#v", graph.Edges)
	}
}

func TestBuildActionGraph_ConditionalAndFallbackRoutes(t *testing.T) {
	conditional, err := BuildActionGraph(nil, &ConditionalExpr{
		Cond: &Atom{Name: "check"},
		Then: &Atom{Name: "accept"},
		Else: &Atom{Name: "reject"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"condition", "true", "false", "result:true", "result:false"} {
		if !hasGraphEdge(conditional, kind) {
			t.Fatalf("conditional graph lacks %q route: %#v", kind, conditional.Edges)
		}
	}

	fallback, err := BuildActionGraph(nil, &FallbackExpr{
		L: &Atom{Name: "primary"},
		R: &Atom{Name: "backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"first-attempt", "on-failure", "success", "recovered"} {
		if !hasGraphEdge(fallback, kind) {
			t.Fatalf("fallback graph lacks %q route: %#v", kind, fallback.Edges)
		}
	}
}

func TestBuildActionGraph_OmitsParameterValues(t *testing.T) {
	graph, err := BuildActionGraph(nil, &Atom{
		Name:      "http.request",
		Params:    map[string]string{"authorization": "do-not-print-this"},
		Modifiers: []string{"timeout=5s", "retry=3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-print-this") || strings.Contains(string(encoded), "5s") {
		t.Fatalf("graph leaked parameter/modifier values: %s", encoded)
	}
	if len(graph.Nodes) != 1 || strings.Join(graph.Nodes[0].Modifiers, ",") != "timeout,retry" {
		t.Fatalf("unexpected action node: %#v", graph.Nodes)
	}
}

func TestCompileAction_CapturesGraphOnlyWhenRequested(t *testing.T) {
	act := action.New("demo.noop", func(_ context.Context, in any) (any, error) {
		return in, nil
	}).Build()
	resolver, err := NewDynamicResolver(action.Library{Name: "demo", Actions: []action.AnyAction{act}})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(opts ...CompileOption) CompileRes {
		t.Helper()
		res, compileErr := CompileAction(
			resolver,
			NewDirectiveTable(),
			NewModifierTable(),
			NewOperatorTable(),
			DefaultPrimaryExtensions(),
			opts...,
		).Do(context.Background(), CompileReq{Source: "demo.noop"})
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		return res
	}

	if got := compile().ActionGraph; got != nil {
		t.Fatalf("default compile returned an action graph: %#v", got)
	}
	if got := compile(WithActionGraph()).ActionGraph; got == nil || len(got.Nodes) != 1 {
		t.Fatalf("opt-in compile graph = %#v, want one action node", got)
	}
}

func hasGraphEdge(graph *ActionGraph, kind string) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			return true
		}
	}
	return false
}

func hasGraphEdgeFromTo(graph *ActionGraph, kind, from, to string) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind && edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}
