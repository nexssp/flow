package syntax

import (
	"testing"

	compiler "github.com/nexssp/flow/compiler"
)

func TestActionGraph_DescribesNamedParallelBranches(t *testing.T) {
	expr := &NamedParallelExpr{branches: []namedParallelBranch{
		{label: "first", expr: &compiler.Atom{Name: "one"}},
		{label: "second", expr: &compiler.Atom{Name: "two"}},
	}}
	graph, err := compiler.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 || graph.Nodes[0].Kind != "parallel.fork" {
		t.Fatalf("first graph node = %#v, want parallel fork", graph.Nodes)
	}
	if !hasEdge(graph, "first") || !hasEdge(graph, "second") || !hasEdge(graph, "result:first") {
		t.Fatalf("graph is missing labeled fan-out/join edges: %#v", graph.Edges)
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
