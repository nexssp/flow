package on_error

import (
	"testing"

	"github.com/nexssp/kernel/xerr"

	core "github.com/nexssp/flow/compiler"
)

func TestActionGraph_DescribesRecoveryRoutes(t *testing.T) {
	expr := &ErrorGuardExpr{
		Protected: &core.Atom{Name: "request"},
		Cases: []ErrorGuardCase{
			{Kind: xerr.Kind("Unavailable"), Body: &core.Atom{Name: "recover"}},
			{IsElse: true, Body: &core.Atom{Name: "fallback"}},
		},
	}
	graph, err := core.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"protected", "success", "on Unavailable", "else"} {
		if !hasEdge(graph, kind) {
			t.Fatalf("recovery graph lacks %q route: %#v", kind, graph.Edges)
		}
	}
}

func hasEdge(graph *core.ActionGraph, kind string) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			return true
		}
	}
	return false
}
