package syntax

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

func TestRaceKeyword_UsesKernelRaceAny(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	parser := compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(bundle.Primaries...),
		`race { test.failed, test.winner, test.canceled }`,
	)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse race expression: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	loserStarted := make(chan struct{})
	loserExited := make(chan struct{})

	failed := action.New("test.failed", func(context.Context, any) (any, error) {
		return nil, errors.New("candidate failed")
	}).Build()
	winner := action.New("test.winner", func(ctx context.Context, _ any) (any, error) {
		select {
		case <-loserStarted:
			return "winner", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}).Build()
	canceled := action.New("test.canceled", func(ctx context.Context, _ any) (any, error) {
		close(loserStarted)
		<-ctx.Done()
		close(loserExited)
		return nil, ctx.Err()
	}).Build()
	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	if err := resolver.Mount(action.Library{
		Name:    "race-test",
		Actions: []action.AnyAction{failed, winner, canceled},
	}); err != nil {
		t.Fatalf("mount actions: %v", err)
	}
	if err := expr.Analyze(resolver); err != nil {
		t.Fatalf("analyze race expression: %v", err)
	}
	program, err := compiler.Build(ctx, resolver, nil, expr)
	if err != nil {
		t.Fatalf("build race expression: %v", err)
	}
	output, err := action.InvokeAny(ctx, program, nil)
	if err != nil {
		t.Fatalf("invoke race expression: %v", err)
	}
	if output != "winner" {
		t.Fatalf("race output = %#v, want winner", output)
	}
	select {
	case <-loserExited:
	case <-time.After(time.Second):
		t.Fatal("race returned before the canceled candidate exited")
	}
}

func TestRaceKeyword_RejectsEmptyBlock(t *testing.T) {
	t.Parallel()

	bundle := Bundle(nil)
	parser := compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(bundle.Primaries...),
		`race {}`,
	)
	if _, err := parser.Parse(); err == nil {
		t.Fatal("parsing empty race block succeeded; want an error")
	}
}

func TestRaceExpr_DescribesRaceCandidatesInActionGraph(t *testing.T) {
	t.Parallel()

	expr := &RaceExpr{candidates: []compiler.Expr{
		&compiler.Atom{Name: "first"},
		&compiler.Atom{Name: "second"},
	}}
	graph, err := compiler.BuildActionGraph(nil, expr)
	if err != nil {
		t.Fatalf("build action graph: %v", err)
	}
	if len(graph.Nodes) == 0 || graph.Nodes[0].Kind != "race.fork" {
		t.Fatalf("first graph node = %#v, want race.fork", graph.Nodes)
	}
	if !hasEdge(graph, "candidate_1") || !hasEdge(graph, "candidate_2") {
		t.Fatalf("race candidate edges missing: %#v", graph.Edges)
	}
}
