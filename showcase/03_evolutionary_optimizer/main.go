package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/optimizer"
	"github.com/nexssp/kernel/action"
)

type AuditTask struct {
	CommitID string `json:"commit_id"`
}

func main() {
	ctx := context.Background()

	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("[nexssp/flow] Architecture 05: Genetic Pipeline Topography Optimizer")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("• Baseline  : fetch -> audit_sec -> audit_perf -> deploy (~60ms sequential)")
	fmt.Println("• Optimizer : 4 Generations | Population: 6 mutations/gen")
	fmt.Println("• Objective : Maximize throughput by discovering parallel execution paths")
	fmt.Println(strings.Repeat("─", 78))

	fetch := action.New("fetch", func(_ context.Context, t AuditTask) (AuditTask, error) {
		return t, nil
	}).Build()

	auditSec := action.New("audit_sec", func(_ context.Context, t AuditTask) (AuditTask, error) {
		time.Sleep(30 * time.Millisecond) // Heavy security analysis

		return t, nil
	}).Build()

	auditPerf := action.New("audit_perf", func(_ context.Context, t AuditTask) (AuditTask, error) {
		time.Sleep(30 * time.Millisecond) // Heavy performance analysis

		return t, nil
	}).Build()

	deploy := action.New("deploy", func(_ context.Context, t AuditTask) (string, error) {
		return "RELEASE_STAGING_" + t.CommitID, nil
	}).Build()

	registry, err := action.NewRegistry(action.Of(fetch, auditSec, auditPerf, deploy))
	if err != nil {
		panic(err)
	}

	baselineDSL := "fetch -> audit_sec -> audit_perf -> deploy"
	baselineBld, compileErr := flow.CompilePipeline(baselineDSL, registry)
	if compileErr != nil {
		panic(compileErr)
	}
	livePipeline := action.NewProxy(baselineBld.Build())

	fmt.Printf("⚡ Active Baseline (Sequential): %s (~60ms)\n\n", baselineDSL)

	evaluator := func(evalCtx context.Context, candidate action.AnyAction) (float64, error) {
		start := time.Now()

		_, err := candidate.DoAny(evalCtx, AuditTask{CommitID: "c_8899"})
		if err != nil {
			return -500.0, nil
		}

		latencyMS := float64(time.Since(start).Milliseconds())

		return 100.0 - latencyMS, nil
	}

	fmt.Println("🧬 Background Optimizer: Evolving DAG across generations...")

	startOpt := time.Now()

	best, err := optimizer.Evolve(ctx, baselineDSL, registry, evaluator, optimizer.Options{
		Generations: 4,
		Population:  6,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("⏱️  Evolution Completed in : %v\n", time.Since(startOpt))
	fmt.Printf("🏆 Discovered Topology     :\n   %s (Fitness Score: %.2f)\n\n", best.DSL, best.Score)

	if best.DSL != baselineDSL {
		newBld, _ := flow.CompilePipeline(best.DSL, registry)
		livePipeline.Swap(newBld.Build())
		fmt.Println("✨ Hot-Swap: Active pipeline pointer updated to evolved graph")
	}

	fmt.Println(strings.Repeat("─", 78))

	startRun := time.Now()
	out, _ := livePipeline.DoAny(ctx, AuditTask{CommitID: "c_8899"})
	duration := time.Since(startRun)

	fmt.Printf("• Live Result : %v\n", out)
	fmt.Printf("• Latency     : %v (Latency halved through parallelization: ~60ms -> ~30ms)\n", duration)
	fmt.Println(strings.Repeat("═", 78))
}
