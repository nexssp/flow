package on_error

import (
	"testing"

	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

func TestActionGraph_DescribesRecoveryRoutes(t *testing.T) {
	expr := &ErrorGuardExpr{
		Protected: &compiler.Atom{Name: "request"},
		Cases: []ErrorGuardCase{
			{Kind: xerr.Kind("Unavailable"), Body: &compiler.Atom{Name: "recover"}},
			{IsElse: true, Body: &compiler.Atom{Name: "fallback"}},
		},
	}
	graph, err := compiler.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"protected", "success", "on Unavailable", "else"} {
		if !hasEdge(graph, kind) {
			t.Fatalf("recovery graph lacks %q route: %#v", kind, graph.Edges)
		}
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
