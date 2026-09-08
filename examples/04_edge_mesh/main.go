package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	// No defer cancel() – we call cancel manually on all exit paths.

	telemetry := flow.NewEdgeTelemetryRing()
	if telemetry == nil {
		fmt.Fprintln(os.Stderr, "❌ Preflight Check: Telemetry Buffer initialization failed.")
		cancel()
		os.Exit(1)
	}

	edgeEU := flow.BuildEdgeMeshAction(flow.RegionEU, "node-eu-01", telemetry)
	edgeUS := flow.BuildEdgeMeshAction(flow.RegionUS, "node-us-01", telemetry)
	edgeAsia := flow.BuildEdgeMeshAction(flow.RegionAsia, "node-asia-01", telemetry)

	ingress := action.New("ingress.auth", func(_ context.Context, req map[string]any) (map[string]any, error) {
		txID, _ := req["tx_id"].(string)
		amount, _ := req["amount"].(int)

		if txID == "" || amount <= 0 {
			return nil, fmt.Errorf("ingress: invalid/missing payload fields")
		}

		return map[string]any{
			"tx_id":  txID,
			"amount": amount,
			"region": req["region"],
			"status": "AUTHENTICATED",
		}, nil
	}).Build()

	aggregate := action.New("mesh.aggregate", func(_ context.Context, req map[string]any) (map[string]any, error) {
		if len(req) == 0 {
			return nil, fmt.Errorf("aggregate: no upstream data received from nodes")
		}
		return map[string]any{
			"region_count": len(req),
			"raw_reports":  req,
		}, nil
	}).Build()

	registry := flow.NewRegistry(ingress, edgeEU, edgeUS, edgeAsia, aggregate)

	dsl := `
		ingress.auth:retry=2:timeout=100ms:idempotent
		-> ( edge.eu-central-1:timeout=200ms & edge.us-east-1:timeout=200ms & edge.ap-southeast-1:timeout=200ms )
		-> mesh.aggregate
		-> { regions_processed: region_count, global_status: "VERIFIED" }
	`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Pipeline parsing or compilation error: %v\n", err)
		cancel()
		os.Exit(1)
	}

	start := time.Now()
	result, err := builder.Build().Do(ctx, map[string]any{
		"tx_id":  "tx_9988776655",
		"amount": 49900,
		"region": "eu",
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "❌ Request aborted (Limit of 2s exceeded): %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "❌ Core execution failure: %v\n", err)
		}
		cancel()
		os.Exit(1)
	}

	cancel()
	fmt.Printf("✅ Edge result: %v\n", result)
	fmt.Printf("⚡ Total multi-edge latency: %v\n", time.Since(start))

	var events [16]flow.EdgeEventSlot
	n := telemetry.BatchDrain(events[:])
	fmt.Printf("📊 Captured %d telemetry events:\n", n)
	for i := 0; i < n; i++ {
		ev := events[i]
		fmt.Printf("   - node=%-15s latency=%dns status=%d\n",
			string(ev.EdgeID[:10]), ev.LatencyNs, ev.StatusCode,
		)
	}
}
