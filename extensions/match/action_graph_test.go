package match

import (
	"testing"

	compiler "github.com/nexssp/flow/compiler"
)

func TestActionGraph_DescribesMatchRoutes(t *testing.T) {
	expr := &Expr{Cases: []Case{
		{Condition: ".ok", Body: &compiler.Atom{Name: "accept"}},
		{IsDefault: true, Body: &compiler.Atom{Name: "reject"}},
	}}
	graph, err := compiler.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 || graph.Nodes[0].Kind != "match" {
		t.Fatalf("first graph node = %#v, want match dispatcher", graph.Nodes)
	}
	if !hasEdge(graph, "case 1") || !hasEdge(graph, "default") {
		t.Fatalf("graph is missing case/default routes: %#v", graph.Edges)
	}
}

func hasEdge(graph *compiler.ActionGraph, kind string) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			return true
		}
	}
	return false
}
