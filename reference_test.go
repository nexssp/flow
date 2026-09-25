package flow_test

import (
	"context"
	"maps"
	"os"
	"sync"
	"testing"

	"github.com/nexssp/flow"
	flowcontracts "github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

func init() {
	// Register test directives for domain blocks used in reference.nflow
	// so flow does not depend on the external ai/sandbox module.
	directives.Register(stubDomainDirective{"llm"})
	directives.Register(stubDomainDirective{"sandbox"})
}

type stubDomainDirective struct {
	name string
}

func (d stubDomainDirective) Name() string { return d.name }

func (d stubDomainDirective) Apply(ctx *directives.Context, lines []string, i int) (int, error) {
	_, _, next, err := directives.SplitBlock(lines, i)
	if err != nil {
		return 0, err
	}
	if ctx.Out.Declarations == nil {
		ctx.Out.Declarations = make(map[string]any)
	}
	ctx.Out.Declarations[d.name] = true
	return next, nil
}

// approveAll implements flow.ApprovalGate and approves every request.
//
// NOTE: Because reference.nflow contains a ternary
// (`approved == true ? merger.merge : human.review`), ParseArrowDSL
// reports NeedsPipelineError and NewExecuteAction falls back to
// runAsPipeline. That path does NOT consult the compiler's ApprovalGate
// — so approveAll is never invoked by this test. It is kept here to
// document the expected wiring and to keep the test compiling against
// the same shape the runner uses.
type approveAll struct{}

func (approveAll) Check(_ context.Context, _, _, _ string) error { return nil }

// TestReferencePipeline exercises the .nflow file end-to-end through the
// *pipeline* compiler path (not the DAG compiler path).
//
// What this test proves:
//   - Preprocess correctly parses every directive in the file.
//   - Named pipelines (@pipeline) register and compose.
//   - DSL operators work: ->, &, { projection }, ? :.
//   - :provider=/:typed= modifiers and @{...} inline args reach the
//     underlying actions, with correct .path resolution.
//   - The ternary deterministically selects merger.merge.
//
// What it does NOT exercise (see graph_test.go for those):
//   - DAG layer counting, edge-level journaling, gate enforcement.
//   - Profile-hook resolution (untrusted_input requires
//     guard.prompt_injection and guard.pii_redact to be registered;
//     runAsPipeline skips resolveProfileHooks entirely).
func TestReferencePipeline(t *testing.T) {
	t.Parallel()

	const flowPath = "testdata/reference.nflow"

	if _, err := os.Stat(flowPath); err != nil {
		t.Skipf("%s is missing: %v", flowPath, err)
	}

	// ─── 1. preprocess ────────────────────────────────────────────────
	pre, err := flow.Preprocess(flowPath)
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	assertPreprocess(t, pre)

	// ─── 2. mock registry + trace ─────────────────────────────────────
	trace := &callTrace{}
	reg := buildReferenceRegistry(t, trace)

	// ─── 3. register named pipelines ──────────────────────────────────
	reg, err = flow.RegisterPipelines(t.Context(), reg, pre.Pipelines)
	if err != nil {
		t.Fatalf("RegisterPipelines: %v", err)
	}

	// ─── 4. compile ───────────────────────────────────────────────────
	compiler := flow.NewCompiler(reg,
		flow.WithApprovalGate(approveAll{}),
	)
	execAct := flow.NewExecuteAction(compiler)

	// ─── 5. execution context ─────────────────────────────────────────
	runCtx := xctx.WithExecutionID(context.Background(), "run_ref_001")
	runCtx = flow.WithGates(runCtx, pre.Gates)

	// Pools from @pool — reference.nflow does not use dispatch, so this
	// is currently a no-op. Kept to document the wiring for future DSL
	// revisions that add dispatch nodes.
	if len(pre.Pools) > 0 {
		pools := make(map[string][]string, len(pre.Pools))
		for _, p := range pre.Pools {
			pools[p.Name] = p.Members
		}
		runCtx = flowcontracts.WithPools(runCtx, pools)
	}

	// ─── 6. execute ───────────────────────────────────────────────────
	initial := map[string]any{
		"session_id": "sess_ref_001",
		"ticket_id":  "TICKET-REF-1",
		"ticket":     "Add validation to user input handler",
		"feedback":   "",
	}

	res, err := execAct.Do(runCtx, flow.GraphExecReq{
		DSL:            pre.DSL,
		Profile:        flow.Profile(pre.Profile),
		Name:           "reference.nflow",
		InitialPayload: initial,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// ─── 7. verify ────────────────────────────────────────────────────
	assertOutput(t, res)
	assertTrace(t, trace)
}

// ─────────────────────────────────────────────────────────────────────────
// Preprocess assertions
// ─────────────────────────────────────────────────────────────────────────

func assertPreprocess(t *testing.T, pre *flow.Preprocessed) {
	t.Helper()

	if got, want := pre.Description, "Reference pipeline — exercises every directive"; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}

	if got, want := len(pre.Asserts), 2; got != want {
		t.Fatalf("Asserts: got %d, want %d", got, want)
	}

	if got, want := pre.Profile, "untrusted_input"; got != want {
		t.Errorf("Profile = %q, want %q", got, want)
	}

	caps := pre.Capabilities
	if len(caps.LLM) != 1 || caps.LLM[0] != "deepseek" {
		t.Errorf("Capabilities.LLM = %v, want [deepseek]", caps.LLM)
	}
	if len(caps.Sandbox) != 1 || caps.Sandbox[0] != "golang:1.26" {
		t.Errorf("Capabilities.Sandbox = %v, want [golang:1.26]", caps.Sandbox)
	}
	if len(caps.Network) != 1 || caps.Network[0] != "api.github.com" {
		t.Errorf("Capabilities.Network = %v, want [api.github.com]", caps.Network)
	}

	wantConfig := map[string]string{
		"budget_usd": "0.50",
		"approval":   "danger",
		"max_tokens": "4096",
	}
	for k, want := range wantConfig {
		if got := pre.Config[k]; got != want {
			t.Errorf("Config[%q] = %q, want %q", k, got, want)
		}
	}

	if got, want := len(pre.Gates), 2; got != want {
		t.Fatalf("Gates: got %d, want %d", got, want)
	}
	if pre.Gates[0].Kind != "on" || pre.Gates[0].Expr != "high_risk" {
		t.Errorf("Gates[0] = %+v, want {on high_risk}", pre.Gates[0])
	}
	if pre.Gates[1].Kind != "when" {
		t.Errorf("Gates[1].Kind = %q, want \"when\"", pre.Gates[1].Kind)
	}

	if len(pre.Pools) != 1 || pre.Pools[0].Name != "experts" {
		t.Fatalf("Pools = %+v, want [{Name:experts ...}]", pre.Pools)
	}
	if got, want := len(pre.Pools[0].Members), 2; got != want {
		t.Errorf("Pool[experts].Members: got %d, want %d", got, want)
	}

	if got, want := len(pre.Pipelines), 2; got != want {
		t.Fatalf("Pipelines: got %d, want %d", got, want)
	}
	names := map[string]bool{}
	for _, p := range pre.Pipelines {
		names[p.Name] = true
	}
	if !names["security_scan"] || !names["test_scan"] {
		t.Errorf("Pipelines missing security_scan or test_scan: got %v", names)
	}

	// AI + sandbox declarations must have been recorded by their domain
	// directives. We don't assert the concrete type — that would couple
	// this test to ai/llm/directives.
	if _, ok := pre.Declarations["llm"]; !ok {
		t.Error("Declarations[llm] missing")
	}
	if _, ok := pre.Declarations["sandbox"]; !ok {
		t.Error("Declarations[sandbox] missing")
	}

	if len(pre.Requires) > 0 {
		t.Logf("reference.nflow declares %d @require directive(s): %+v",
			len(pre.Requires), pre.Requires)
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Execution trace
// ─────────────────────────────────────────────────────────────────────────

// callTrace records what each reference action saw. Snapshots are taken
// with maps.Clone before the mock mutates its request, so the trace is
// a true historical record regardless of what the action does next.
//
// The mutex guards against a future revision of reference.nflow that
// fans out over architect or critic — the mocks would then run
// concurrently and a bare map assignment would race.
type callTrace struct {
	mu           sync.Mutex
	architectSaw map[string]any
	criticSaw    map[string]any
	mergeCalled  bool
	reviewCalled bool
}

func (c *callTrace) recordArchitect(in map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.architectSaw = maps.Clone(in)
}

func (c *callTrace) recordCritic(in map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.criticSaw = maps.Clone(in)
}

func (c *callTrace) recordMerge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mergeCalled = true
}

func (c *callTrace) recordReview() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reviewCalled = true
}

func (c *callTrace) snapshot() (architect, critic map[string]any, merge, review bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.architectSaw, c.criticSaw, c.mergeCalled, c.reviewCalled
}

func assertTrace(t *testing.T, trace *callTrace) {
	t.Helper()

	architect, critic, merge, review := trace.snapshot()

	// architect received @{ goal: .ticket } resolved from parser.spec's
	// output, plus the :typed= modifier injected as a request field.
	if architect == nil {
		t.Fatal("architect was never invoked")
	}
	if got, want := architect["goal"], "Add validation to user input handler"; got != want {
		t.Errorf("architect.goal = %v, want %q", got, want)
	}
	if got, want := architect["typed"], "ArchitectPlan"; got != want {
		t.Errorf("architect.typed = %v, want %q (from :typed=ArchitectPlan)", got, want)
	}
	if _, ok := architect["source_code"]; !ok {
		t.Error("architect did not receive source_code from parser.spec")
	}
	if got, want := architect["session_id"], "sess_ref_001"; got != want {
		t.Errorf("architect.session_id = %v, want %q", got, want)
	}

	// critic received @{ review: .source_code } and :typed=Verdict.
	if critic == nil {
		t.Fatal("critic was never invoked")
	}
	if got, want := critic["review"], "package main\nfunc main() {}"; got != want {
		t.Errorf("critic.review = %q, want %q", got, want)
	}
	if got, want := critic["typed"], "Verdict"; got != want {
		t.Errorf("critic.typed = %v, want %q (from :typed=Verdict)", got, want)
	}

	// Ternary: architect and critic both set approved=true, so the DSL
	// must deterministically select merger.merge.
	if !merge {
		t.Error("merger.merge was not called; ternary did not follow the approved branch")
	}
	if review {
		t.Error("human.review was called; ternary followed the wrong branch")
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Registry
// ─────────────────────────────────────────────────────────────────────────

// buildReferenceRegistry registers every action the reference pipeline
// calls. The mocks are deterministic and side-effect-free: no real
// provider, no network, no subprocess.
//
// Allocation discipline: parser.spec builds a fresh map (its output is
// the pipeline's working set). architect and critic snapshot their
// input into the trace, then mutate the map in place — safe because
// parser.spec handed them a fresh map that nobody else holds. Leaf
// nodes return small fresh maps. No maps.Clone on the working path.
func buildReferenceRegistry(t *testing.T, trace *callTrace) *action.Registry {
	t.Helper()

	parser := action.New("parser.spec", func(_ context.Context, in map[string]any) (map[string]any, error) {
		// Fresh map — this becomes the pipeline's working set and will
		// be mutated in place by downstream nodes.
		return map[string]any{
			"session_id":  in["session_id"],
			"ticket_id":   in["ticket_id"],
			"ticket":      in["ticket"],
			"feedback":    in["feedback"],
			"source_code": "package main\nfunc main() {}",
			"test_code":   "package main\nimport \"testing\"\nfunc TestX(t *testing.T) {}",
		}, nil
	}).Build()

	architect := action.New("architect", func(_ context.Context, in map[string]any) (map[string]any, error) {
		trace.recordArchitect(in) // snapshots with maps.Clone
		in["approved"] = true     // safe: parser.spec's fresh map
		return in, nil
	}).Build()

	security := action.New("scanner.security", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"exit_code": 0, "result": "clean"}, nil
	}).Build()

	tester := action.New("scanner.test", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"exit_code": 0, "stdout": "PASS"}, nil
	}).Build()

	critic := action.New("critic", func(_ context.Context, in map[string]any) (map[string]any, error) {
		trace.recordCritic(in) // snapshots with maps.Clone
		in["approved"] = true  // idempotent — already true from architect
		return in, nil
	}).Build()

	merge := action.New("merger.merge", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		trace.recordMerge()
		return map[string]any{"status": "approved"}, nil
	}).Build()

	humanReview := action.New("human.review", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		trace.recordReview()
		return map[string]any{"status": "pending"}, nil
	}).Build()

	reg, err := action.NewRegistry(action.Of(
		parser, architect, security, tester, critic, merge, humanReview,
	))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}

// ─────────────────────────────────────────────────────────────────────────
// Final output
// ─────────────────────────────────────────────────────────────────────────

func assertOutput(t *testing.T, res flow.GraphExecRes) {
	t.Helper()

	// The pipeline takes the runAsPipeline fallback (see test header),
	// which hardcodes LayersRun=1 and reports GraphName=req.Name.
	if got, want := res.LayersRun, 1; got != want {
		t.Errorf("LayersRun = %d, want %d (pipeline path)", got, want)
	}
	if got, want := res.GraphName, "reference.nflow"; got != want {
		t.Errorf("GraphName = %q, want %q", got, want)
	}

	// The final node is the ternary. Both mocks set approved=true, so
	// merger.merge must have produced status="approved".
	status, ok := res.Outputs["status"].(string)
	if !ok {
		t.Fatalf("final output has no status field; keys: %v", sortedKeys(res.Outputs))
	}
	if got, want := status, "approved"; got != want {
		t.Errorf("final status = %q, want %q", got, want)
	}
}
