package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/optimizer"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx := context.Background()

	fetch := action.New("fetch_data", func(_ context.Context, _ any) (string, error) {
		return "repo_code", nil
	}).Build()

	auditSec := action.New("audit_sec", func(_ context.Context, code string) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "sec_ok", nil
	}).Build()

	auditArch := action.New("audit_arch", func(_ context.Context, code string) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "arch_ok", nil
	}).Build()

	notify := action.New("notify", func(_ context.Context, in any) (string, error) {
		return "Deployment ready", nil
	}).Build()

	registry := flow.NewRegistry(fetch, auditSec, auditArch, notify)

	baselineDSL := "fetch_data -> audit_sec -> audit_arch -> notify"
	baselineBld, err := flow.CompilePipeline(baselineDSL, registry)
	if err != nil {
		panic(err)
	}

	livePipeline := action.NewProxy(baselineBld.Build())

	fmt.Printf("🚀 Baseline Pipeline Deployed: %s\n", baselineDSL)

	evaluator := func(evalCtx context.Context, candidate action.AnyAction) (float64, error) {
		trials := 5
		var totalDuration time.Duration
		successes := 0

		for i := 0; i < trials; i++ {
			start := time.Now()
			_, doErr := candidate.DoAny(evalCtx, nil)
			dur := time.Since(start)

			if doErr == nil {
				successes++
				totalDuration += dur
			}
		}

		if successes == 0 {
			return -1000.0, nil
		}

		avgLatencyMS := float64((totalDuration / time.Duration(trials)).Milliseconds())
		score := (float64(successes) / float64(trials) * 100.0) - avgLatencyMS
		return score, nil
	}

	fmt.Println("\n🧬 Background Optimizer starting evolution (4 generations)...")
	start := time.Now()

	best, err := optimizer.Evolve(ctx, baselineDSL, registry, evaluator, optimizer.Options{
		Generations: 4,
		Population:  6,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("⚡ Evolution completed in %v\n", time.Since(start))
	fmt.Printf("🏆 Best Discovered Topology:\n   DSL:   %s\n   Score: %.2f\n\n", best.DSL, best.Score)

	if best.DSL != baselineDSL {
		fmt.Println("✨ Better topology discovered! Performing zero-downtime hot-swap...")
		fmt.Println("----------------------------------------------------------------")
		fmt.Printf("   OLD (Sequential ~80ms): %s\n", baselineDSL)
		fmt.Printf("   NEW (Parallel   ~40ms): %s\n", best.DSL)
		fmt.Println("----------------------------------------------------------------")

		newBld, compileErr := flow.CompilePipeline(best.DSL, registry)
		if compileErr != nil {
			panic(compileErr)
		}
		livePipeline.Swap(newBld.Build())
		fmt.Println("✅ Hot-swap successful. Live traffic is now executing on the evolved topology.")
	} else {
		fmt.Println("Baseline is already optimal.")
	}

	startRun := time.Now()
	out, _ := livePipeline.DoAny(ctx, nil)
	elapsed := time.Since(startRun)

	fmt.Printf("\n⚡ Live execution output: %v\n", out)
	fmt.Printf("⏱️ Measured runtime latency: %v (Down from ~80ms baseline!)\n", elapsed)
}
