package syntax

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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

func TestRaceKeyword_BoundsActiveCandidatesAndStartsQueuedCandidates(t *testing.T) {
	t.Parallel()

	limit := action.DefaultAsyncMaxConcurrency
	candidateCount := limit + 4
	names := make([]string, candidateCount)
	release := make([]chan struct{}, candidateCount)
	actions := make([]action.AnyAction, candidateCount)
	var active, peak atomic.Int32
	started := make(chan struct{}, candidateCount)
	for index := range candidateCount {
		name := fmt.Sprintf("test.candidate_%d", index)
		names[index] = name
		release[index] = make(chan struct{})
		candidateIndex := index
		actions[index] = action.New(name, func(ctx context.Context, _ any) (any, error) {
			current := active.Add(1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release[candidateIndex]:
			case <-ctx.Done():
			}
			active.Add(-1)
			return nil, fmt.Errorf("candidate %d failed", candidateIndex)
		}).Build()
	}

	source := "race { " + strings.Join(names, ", ") + " }"
	bundle := Bundle(nil)
	parser := compiler.NewParserWithPrimaries(
		context.Background(),
		compiler.NewOperatorTable(bundle.Operators...),
		compiler.NewPrimaryExtensionTable(bundle.Primaries...),
		source,
	)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse race expression: %v", err)
	}
	resolver, err := compiler.NewDynamicResolver()
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	if err := resolver.Mount(action.Library{Name: "race-test", Actions: actions}); err != nil {
		t.Fatalf("mount candidate actions: %v", err)
	}
	if err := expr.Analyze(resolver); err != nil {
		t.Fatalf("analyze race expression: %v", err)
	}
	program, err := compiler.Build(context.Background(), resolver, nil, expr)
	if err != nil {
		t.Fatalf("build race expression: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, invokeErr := action.InvokeAny(ctx, program, nil)
		done <- invokeErr
	}()

	startedCount := 0
	for startedCount < limit {
		select {
		case <-started:
			startedCount++
		case <-ctx.Done():
			t.Fatalf("only %d candidates started before context ended", startedCount)
		}
	}
	close(release[0])
	select {
	case <-started:
		startedCount++
	case <-ctx.Done():
		t.Fatal("queued candidate did not start after a worker slot opened")
	}
	for index := 1; index < candidateCount; index++ {
		close(release[index])
	}
	for startedCount < candidateCount {
		select {
		case <-started:
			startedCount++
		case <-ctx.Done():
			t.Fatalf("only %d of %d candidates started", startedCount, candidateCount)
		}
	}
	if err := <-done; err == nil {
		t.Fatal("race succeeded; want every candidate to fail")
	}
	if got := int(peak.Load()); got != limit {
		t.Fatalf("peak active candidates = %d, want default limit %d", got, limit)
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
