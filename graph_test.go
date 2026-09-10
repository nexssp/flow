package flow_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/journal"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type mockApprovalGate struct {
	mu        sync.RWMutex
	approvals map[string]bool
}

func newMockApprovalGate() *mockApprovalGate {
	return &mockApprovalGate{
		approvals: make(map[string]bool),
	}
}

func (m *mockApprovalGate) Allow(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvals[token] = true
}

func (m *mockApprovalGate) Check(_ context.Context, _ string, _ string, token string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if token != "" && m.approvals[token] {
		return nil
	}

	return xerr.Forbidden("approval required or token invalid")
}

func TestGraph_ArrowDSL_Parsing(t *testing.T) {
	t.Parallel()

	dsl := "srcpack.pack:arch#pkg~mocks@security -> (sec.audit & arch.review) -> gate.synthesizer"
	def, err := flow.ParseArrowDSL("test_pipeline", dsl)
	if err != nil {
		t.Fatalf("ParseArrowDSL failed: %v", err)
	}

	if len(def.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(def.Nodes))
	}
	if len(def.Edges) != 4 {
		t.Fatalf("expected 4 edges (1 -> 2 fan-out, 2 -> 1 fan-in), got %d", len(def.Edges))
	}

	first := def.Nodes[0]
	if first.Capability != "srcpack.pack" {
		t.Errorf("expected capability 'srcpack.pack', got %q", first.Capability)
	}
	if first.Params["profile"] != "arch" {
		t.Errorf("expected profile 'arch', got %v", first.Params["profile"])
	}
	if first.Params["prompt"] != "security" {
		t.Errorf("expected prompt 'security', got %v", first.Params["prompt"])
	}
}

func TestGraph_CompilerAndExecutionTopology(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var packCalls, secCalls, archCalls, gateCalls atomic.Int32

	actPack := action.New("mock.pack", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		packCalls.Add(1)

		return map[string]any{"content": "package auth\nfunc Login() {}"}, nil
	}).Build()

	actSec := action.New("mock.sec", func(_ context.Context, _ map[string]any) (string, error) {
		secCalls.Add(1)

		return "Security: OK", nil
	}).Build()

	actArch := action.New("mock.arch", func(_ context.Context, _ map[string]any) (string, error) {
		archCalls.Add(1)

		return "Arch: Clean", nil
	}).Build()

	actGate := action.New("mock.gate", func(_ context.Context, _ map[string]any) (bool, error) {
		gateCalls.Add(1)

		return true, nil
	}).Build()

	registry := flow.NewRegistry(actPack, actSec, actArch, actGate)
	compiler := flow.NewCompiler(registry)
	execAct := flow.NewExecuteAction(compiler)

	res, err := flow.Execute[flow.GraphExecReq, flow.GraphExecRes](ctx, execAct, flow.GraphExecReq{
		DSL: "mock.pack -> (mock.sec & mock.arch) -> mock.gate",
	})
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if res.LayersRun != 3 {
		t.Errorf("expected 3 topological layers (pack -> parallel fan-out -> gate), got %d", res.LayersRun)
	}
	if packCalls.Load() != 1 || secCalls.Load() != 1 || archCalls.Load() != 1 || gateCalls.Load() != 1 {
		t.Fatalf("unexpected call counts: pack=%d, sec=%d, arch=%d, gate=%d",
			packCalls.Load(), secCalls.Load(), archCalls.Load(), gateCalls.Load())
	}
}

func TestGraph_MultiBranchConditionalJournaling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var codeCalls, fallbackCalls atomic.Int32

	classifyAct := action.New("classify", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"intent": "code"}, nil
	}).Build()

	codeAct := action.New("code_cap", func(_ context.Context, _ map[string]any) (string, error) {
		codeCalls.Add(1)

		return "code executed", nil
	}).Build()

	fallbackAct := action.New("fallback_cap", func(_ context.Context, _ map[string]any) (string, error) {
		fallbackCalls.Add(1)

		return "fallback executed", nil
	}).Build()

	registry := flow.NewRegistry(classifyAct, codeAct, fallbackAct)
	journal := journal.NewMemoryBranchJournal()
	compiler := flow.NewCompiler(registry, flow.WithJournal(journal))

	def := flow.GraphDefinition{
		APIVersion: flow.APIVersion,
		Kind:       "Graph",
		Metadata:   flow.Metadata{Name: "multi_branch_router", Version: "1.0.0"},
		Nodes: []flow.NodeSpec{
			{ID: "classify", Capability: "classify"},
			{ID: "code_node", Capability: "code_cap"},
			{ID: "fallback_node", Capability: "fallback_cap"},
		},
		Edges: []flow.EdgeSpec{
			{From: "classify", To: "code_node", When: `state.intent == "code"`, Priority: 1},
			{From: "classify", To: "fallback_node", Otherwise: true, Priority: 99},
		},
	}

	dagInst, _, err := compiler.Compile(ctx, def)
	if err != nil {
		t.Fatalf("failed to compile graph: %v", err)
	}

	state := flow.AcquireStateFromGraphState(flow.NewState(map[string]any{"intent": "code"}))
	runCtx := action.WithExecutionID(ctx, "run_123")
	outState, err := dagInst.Execute(runCtx, state)
	if err != nil {
		t.Fatalf("first DAG run failed: %v", err)
	}
	defer outState.Release()

	if codeCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("expected code node called once, fallback never: code=%d fallback=%d",
			codeCalls.Load(), fallbackCalls.Load())
	}

	stateMutated := flow.AcquireStateFromGraphState(flow.NewState(map[string]any{"intent": "unknown"}))
	outStateReplayed, err := dagInst.Execute(runCtx, stateMutated)
	if err != nil {
		t.Fatalf("replayed DAG run failed: %v", err)
	}
	defer outStateReplayed.Release()

	if codeCalls.Load() != 2 || fallbackCalls.Load() != 0 {
		t.Fatalf("durable replay failed: code=%d fallback=%d", codeCalls.Load(), fallbackCalls.Load())
	}
}

func TestGraph_ApprovalRequiresGateAtCompileTime(t *testing.T) {
	t.Parallel()

	dummyAct := action.New("dummy", func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}).Build()

	registry := flow.NewRegistry(dummyAct)
	compilerWithoutGate := flow.NewCompiler(registry)

	def := flow.GraphDefinition{
		APIVersion: flow.APIVersion,
		Kind:       "Graph",
		Metadata:   flow.Metadata{Name: "guarded", Version: "1.0.0"},
		Nodes: []flow.NodeSpec{
			{ID: "node1", Capability: "dummy", Approval: true},
		},
	}

	_, _, err := compilerWithoutGate.Compile(context.Background(), def)
	if err == nil || !strings.Contains(err.Error(), "no ApprovalGate was configured") {
		t.Fatalf("expected compile-time rejection when approval required without gate, got: %v", err)
	}

	gate := newMockApprovalGate()
	compilerWithGate := flow.NewCompiler(registry, flow.WithApprovalGate(gate))
	_, _, err = compilerWithGate.Compile(context.Background(), def)
	if err != nil {
		t.Fatalf("expected compilation to succeed with gate, got: %v", err)
	}
}

func TestGraph_ScopedApprovalEnforcement(t *testing.T) {
	t.Parallel()

	dummyAct := action.New("deploy", func(_ context.Context, _ map[string]any) (string, error) {
		return "deployed", nil
	}).Build()

	registry := flow.NewRegistry(dummyAct)
	gate := newMockApprovalGate()
	compiler := flow.NewCompiler(registry, flow.WithApprovalGate(gate))

	def := flow.GraphDefinition{
		APIVersion: flow.APIVersion,
		Kind:       "Graph",
		Metadata:   flow.Metadata{Name: "deploy_graph", Version: "1.0.0"},
		Nodes: []flow.NodeSpec{
			{ID: "deploy_node", Capability: "deploy", Approval: true},
		},
	}

	dagInst, _, err := compiler.Compile(context.Background(), def)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	state := flow.AcquireStateFromGraphState(flow.NewState(nil))
	_, err = dagInst.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected approval failure when token is missing")
	}

	gate.Allow("tok_deploy")

	state2 := flow.AcquireStateFromGraphState(flow.NewState(nil))
	ctx := flow.WithApprovalToken(context.Background(), "tok_deploy")
	out, err := dagInst.Execute(ctx, state2)
	if err != nil {
		t.Fatalf("expected execution to succeed with approved token, got: %v", err)
	}
	defer out.Release()
}
