package flow_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow"
	branchjournal "github.com/nexssp/flow/journal"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test doubles
// ─────────────────────────────────────────────────────────────────────────────

// gateCall records one invocation of mockApprovalGate.Check so tests can
// assert on the action name, argument payload, and token the compiler
// forwarded.
type gateCall struct {
	Action string
	Args   string
	Token  string
}

type mockApprovalGate struct {
	mu        sync.RWMutex
	approvals map[string]bool
	calls     []gateCall
}

func newMockApprovalGate() *mockApprovalGate {
	return &mockApprovalGate{approvals: make(map[string]bool)}
}

func (m *mockApprovalGate) Allow(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvals[token] = true
}

func (m *mockApprovalGate) Check(_ context.Context, actionName, args, token string) error {
	m.mu.Lock()
	m.calls = append(m.calls, gateCall{Action: actionName, Args: args, Token: token})
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if token != "" && m.approvals[token] {
		return nil
	}
	return xerr.Forbidden("approval required or token invalid")
}

func (m *mockApprovalGate) callsSnapshot() []gateCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]gateCall(nil), m.calls...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Arrow DSL — parsing
// ─────────────────────────────────────────────────────────────────────────────

func TestGraph_ArrowDSL_Parsing(t *testing.T) {
	t.Parallel()

	dsl := "srcpack.pack:arch#pkg~mocks@security -> (sec.audit & arch.review) -> gate.synthesizer"

	def, err := flow.ParseArrowDSL("test_pipeline", dsl)
	if err != nil {
		t.Fatalf("ParseArrowDSL failed: %v", err)
	}

	if got, want := len(def.Nodes), 4; got != want {
		t.Fatalf("nodes: got %d, want %d", got, want)
	}
	if got, want := len(def.Edges), 4; got != want {
		t.Fatalf("edges: got %d, want %d (1 fan-out + 2 fan-in)", got, want)
	}

	first := def.Nodes[0]
	if first.Capability != "srcpack.pack" {
		t.Errorf("capability: got %q, want srcpack.pack", first.Capability)
	}
	if first.Params["profile"] != "arch" {
		t.Errorf("profile: got %v, want arch", first.Params["profile"])
	}
	if first.Params["prompt"] != "security" {
		t.Errorf("prompt: got %v, want security", first.Params["prompt"])
	}

	// Params stores []string as `any`, so DeepEqual is required.
	if got, ok := first.Params["targets"].([]string); !ok || !reflect.DeepEqual(got, []string{"pkg"}) {
		t.Errorf("targets: got %#v, want [pkg]", first.Params["targets"])
	}
	if got, ok := first.Params["excludes"].([]string); !ok || !reflect.DeepEqual(got, []string{"mocks"}) {
		t.Errorf("excludes: got %#v, want [mocks]", first.Params["excludes"])
	}
}

// TestGraph_ArrowDSL_InlineArgs documents a known gap: ParseArrowDSL
// copies Profile/Prompt/Targets/Excludes into NodeSpec.Params, but it
// does not propagate AtomExpr.Args (the `@{ key: .path }` payload).
// When that gap is closed, remove the Skip and tighten the assertion.
func TestGraph_ArrowDSL_InlineArgs(t *testing.T) {
	t.Parallel()
	t.Skip("known gap: ParseArrowDSL does not forward AtomExpr.Args into NodeSpec.Params")

	dsl := `agent.fixer @{ goal: .ticket, feedback: .err }`

	def, err := flow.ParseArrowDSL("inline_args", dsl)
	if err != nil {
		t.Fatalf("ParseArrowDSL: %v", err)
	}
	if len(def.Nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(def.Nodes))
	}

	params := def.Nodes[0].Params
	if params["goal"] != ".ticket" {
		t.Errorf("goal: got %v, want .ticket", params["goal"])
	}
	if params["feedback"] != ".err" {
		t.Errorf("feedback: got %v, want .err", params["feedback"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Topology — layers, call counts, output routing
// ─────────────────────────────────────────────────────────────────────────────

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

	registry := action.MustNewRegistry(action.Of(actPack, actSec, actArch, actGate))
	compiler := flow.NewCompiler(registry)
	execAct := flow.NewExecuteAction(compiler)

	res, err := execAct.Do(ctx, flow.GraphExecReq{
		DSL: "mock.pack -> (mock.sec & mock.arch) -> mock.gate",
	})
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if got, want := res.LayersRun, 3; got != want {
		t.Errorf("layers: got %d, want %d (pack → fan-out → gate)", got, want)
	}

	if packCalls.Load() != 1 || secCalls.Load() != 1 || archCalls.Load() != 1 || gateCalls.Load() != 1 {
		t.Fatalf("call counts: pack=%d sec=%d arch=%d gate=%d (want 1 each)",
			packCalls.Load(), secCalls.Load(), archCalls.Load(), gateCalls.Load())
	}

	// Outputs land under dag.OutputKey(nodeID). The gate consumed
	// both parallel branches, so its output must be present.
	gateKey := dag.OutputKey("mock.gate")
	if got := res.Outputs[gateKey]; got != true {
		t.Errorf("gate output: got %v, want true", got)
	}

	// Both fan-out branches must have produced an output key.
	secKey := dag.OutputKey("mock.sec")
	if _, ok := res.Outputs[secKey]; !ok {
		t.Errorf("missing %s; keys: %v", secKey, sortedKeys(res.Outputs))
	}
	archKey := dag.OutputKey("mock.arch")
	if _, ok := res.Outputs[archKey]; !ok {
		t.Errorf("missing %s; keys: %v", archKey, sortedKeys(res.Outputs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Conditional routing + branch journaling
// ─────────────────────────────────────────────────────────────────────────────

func TestGraph_MultiBranchConditionalJournaling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runID := "run_" + t.Name()

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

	registry := action.MustNewRegistry(action.Of(classifyAct, codeAct, fallbackAct))
	branchJournal := branchjournal.NewMemoryBranchJournal()
	compiler := flow.NewCompiler(registry, flow.WithJournal(branchJournal))

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
		t.Fatalf("compile: %v", err)
	}

	runCtx := action.WithExecutionID(ctx, runID)

	// First run — journal is empty, so conditions are evaluated live.
	state := dag.AcquireState()
	state.Set("intent", "code")
	defer state.Release()

	outState, err := dagInst.Execute(runCtx, state.AsRead())
	if err != nil {
		t.Fatalf("first DAG run: %v", err)
	}
	defer outState.Release()

	if codeCalls.Load() != 1 || fallbackCalls.Load() != 0 {
		t.Fatalf("first run: code=%d fallback=%d (want 1/0)",
			codeCalls.Load(), fallbackCalls.Load())
	}

	// The journal must record exactly two decisions for the classify node.
	records, found, err := branchJournal.Get(ctx, runID, "classify")
	if err != nil {
		t.Fatalf("journal.Get: %v", err)
	}
	if !found {
		t.Fatal("journal: no records persisted for classify")
	}
	if len(records) != 2 {
		t.Fatalf("journal: got %d records, want 2 (one per outgoing edge)", len(records))
	}

	var selected []string
	for _, r := range records {
		if r.Status == branchjournal.BranchSelected {
			selected = append(selected, r.To)
		}
	}
	if !reflect.DeepEqual(selected, []string{"code_node"}) {
		t.Fatalf("journal selected: got %v, want [code_node]", selected)
	}

	// Second run — same runID, but the payload is different. The
	// journal must win: the recorded decision is replayed verbatim, so
	// code_cap runs again and fallback_cap is never reached.
	stateMutated := dag.AcquireState()
	stateMutated.Set("intent", "unknown")
	defer stateMutated.Release()

	outStateReplayed, err := dagInst.Execute(runCtx, stateMutated.AsRead())
	if err != nil {
		t.Fatalf("replayed DAG run: %v", err)
	}
	defer outStateReplayed.Release()

	if codeCalls.Load() != 2 || fallbackCalls.Load() != 0 {
		t.Fatalf("replay: code=%d fallback=%d (want 2/0 — journal overrides payload)",
			codeCalls.Load(), fallbackCalls.Load())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Approval — compile-time gate requirement
// ─────────────────────────────────────────────────────────────────────────────

func TestGraph_ApprovalRequiresGateAtCompileTime(t *testing.T) {
	t.Parallel()

	dummyAct := action.New("dummy", func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}).Build()

	registry := action.MustNewRegistry(action.Of(dummyAct))

	def := flow.GraphDefinition{
		APIVersion: flow.APIVersion,
		Kind:       "Graph",
		Metadata:   flow.Metadata{Name: "guarded", Version: "1.0.0"},
		Nodes: []flow.NodeSpec{
			{ID: "node1", Capability: "dummy", Approval: true},
		},
	}

	// No gate configured → compile must fail with a specific message.
	_, _, err := flow.NewCompiler(registry).Compile(context.Background(), def)
	if err == nil || !strings.Contains(err.Error(), "no ApprovalGate was configured") {
		t.Fatalf("want compile-time rejection, got: %v", err)
	}

	// Gate configured → compile succeeds.
	gate := newMockApprovalGate()
	_, _, err = flow.NewCompiler(registry, flow.WithApprovalGate(gate)).Compile(context.Background(), def)
	if err != nil {
		t.Fatalf("want success with gate, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Approval — runtime enforcement + forwarded args
// ─────────────────────────────────────────────────────────────────────────────

func TestGraph_ScopedApprovalEnforcement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dummyAct := action.New("deploy", func(_ context.Context, _ map[string]any) (string, error) {
		return "deployed", nil
	}).Build()

	registry := action.MustNewRegistry(action.Of(dummyAct))
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

	dagInst, _, err := compiler.Compile(ctx, def)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// ── Attempt 1: no token → approval failure ──────────────────────
	state := dag.AcquireState()
	defer state.Release()

	if _, err := dagInst.Execute(ctx, state.AsRead()); err == nil {
		t.Fatal("expected approval failure when token is missing")
	}

	// The gate must have been asked, with the namespaced action name.
	calls := gate.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("gate calls: got %d, want 1", len(calls))
	}
	if calls[0].Action != "flow.deploy_node" {
		t.Errorf("gate action: got %q, want flow.deploy_node", calls[0].Action)
	}
	if calls[0].Token != "" {
		t.Errorf("gate token: got %q, want empty on unapproved attempt", calls[0].Token)
	}

	// ── Attempt 2: approved token → success ─────────────────────────
	gate.Allow("tok_deploy")

	state2 := dag.AcquireState()
	defer state2.Release()

	approvedCtx := xctx.WithApprovalToken(ctx, "tok_deploy")

	out, err := dagInst.Execute(approvedCtx, state2.AsRead())
	if err != nil {
		t.Fatalf("expected success with approved token, got: %v", err)
	}
	defer out.Release()

	calls = gate.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("gate calls after approval: got %d, want 2", len(calls))
	}
	if calls[1].Token != "tok_deploy" {
		t.Errorf("gate token: got %q, want tok_deploy", calls[1].Token)
	}
	if calls[1].Action != "flow.deploy_node" {
		t.Errorf("gate action: got %q, want flow.deploy_node", calls[1].Action)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
