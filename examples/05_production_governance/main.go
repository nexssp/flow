package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type approver struct {
	token string
}

func (a approver) Check(_ context.Context, actionName string, payload string, token string) error {
	fmt.Printf("🔑 Approval check for %s (%d bytes payload)\n", actionName, len(payload))
	if token != a.token {
		return fmt.Errorf("missing approval token")
	}
	fmt.Println("✅ Approval granted")
	return nil
}

func main() {
	ctx := context.Background()

	health := action.New("sre.health", func(_ context.Context, req map[string]any) (map[string]any, error) {
		return map[string]any{
			"degraded": req["degraded"],
			"region":   req["region"],
			"p99_ms":   req["p99_ms"],
		}, nil
	}).Build()

	restart := action.New("sre.restart", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("Service restarted in %s (p99 was %vms)", req["region"], req["p99_ms"]), nil
	}).Build()

	notify := action.New("sre.notify", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("On-call paged for %s", req["region"]), nil
	}).Build()

	registry := flow.NewRegistry(health, restart, notify)

	ledger := flow.NewMemoryCostLedger()
	journal := flow.NewMemoryBranchJournal()

	compiler := flow.NewCompiler(
		registry,
		flow.WithLedger(ledger),
		flow.WithJournal(journal),
		flow.WithApprovalGate(approver{token: "incident-demo-token"}),
	)

	execAction := flow.NewExecuteAction(compiler)

	manifest := `
apiVersion: nexss.ai/v1
kind: Graph
metadata:
  name: production-protection
  version: "1.0.0"
policy:
  budget:
    max_cost_usd_per_run: 0.01
    max_cost_usd_per_day: 0.10
  max_parallel_nodes: 2
nodes:
  - id: health
    kind: tool
    capability: sre.health
    effect: read_only
- id: restart
    kind: tool
    capability: sre.restart
    effect: side_effect
    approval_required: true
    estimated_cost_micros: 3000
    inputs:
    region: region
    p99_ms: p99_ms
  - id: notify
    kind: tool
    capability: sre.notify
    effect: read_only
edges:
  - from: health
    to: restart
    when: 'state.degraded == true'
    priority: 1
  - from: health
    to: notify
    otherwise: true
    priority: 99
`

	runCtx := action.WithExecutionID(ctx, "production-protection-run-01")
	runCtx = flow.WithApprovalToken(runCtx, "incident-demo-token")

	res, err := flow.Execute[flow.GraphExecReq, flow.GraphExecRes](
		runCtx,
		execAction,
		flow.GraphExecReq{
			YAML: manifest,
			InitialPayload: map[string]any{
				"degraded": true,
				"region":   "eu-west-1",
				"p99_ms":   420,
			},
		},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n✅ Graph %s executed in %d layers (%dms)\n", res.GraphName, res.LayersRun, res.DurationMS)
	for key, value := range res.Outputs {
		fmt.Printf("   - %s: %v\n", key, value)
	}

	events, _ := ledger.Events(runCtx, "production-protection-run-01")
	fmt.Printf("💰 Cost ledger records: %d\n", len(events))
}
