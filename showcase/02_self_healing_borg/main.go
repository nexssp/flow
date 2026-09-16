package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type WorkerReq struct {
	Data string `json:"data"`
}

type WorkerRes struct {
	Result string `json:"result"`
}

func main() {
	ctx := context.Background()

	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("[nexssp/flow] Architecture 04: Autonomous AST Mutation & Live Hot-Swap")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("• Pattern   : Dynamic AST rewrite triggered by node failure threshold")
	fmt.Println("• Mechanism : Lock-free atomic pointer exchange (sync/atomic.Value via action.Proxy)")
	fmt.Println(strings.Repeat("─", 78))

	var reqCount int32

	fetch := action.New("fetch", func(_ context.Context, req WorkerReq) (WorkerReq, error) {
		return req, nil
	}).Build()

	// Outage begins on request 4
	fragile := action.New("fragile", func(_ context.Context, req WorkerReq) (WorkerRes, error) {
		if atomic.AddInt32(&reqCount, 1) > 3 {
			return WorkerRes{}, fmt.Errorf("connection refused: upstream cluster down")
		}

		return WorkerRes{Result: req.Data + "_fast_edge"}, nil
	}).Build()

	safe := action.New("safe_fallback", func(_ context.Context, req WorkerReq) (WorkerRes, error) {
		return WorkerRes{Result: req.Data + "_safe_cloud"}, nil
	}).Build()

	registry, err := action.NewRegistry(action.Of(fetch, fragile, safe))
	if err != nil {
		panic(err)
	}

	currentDSL := "fetch -> fragile"

	initialBld, err := flow.CompilePipeline(currentDSL, registry)
	if err != nil {
		panic(err)
	}

	livePipeline := action.NewProxy(initialBld.Build())

	var (
		failCounts     sync.Map
		supervisorHook action.AnyHook
	)

	supervisorHook = action.AnyHook{
		After: func(_ context.Context, _, _ any, err error, meta *action.Meta) {
			if err != nil {
				if !strings.Contains(currentDSL, meta.Name) {
					return
				}

				var count int32
				if v, ok := failCounts.Load(meta.Name); ok {
					count = v.(int32)
				}

				count++
				failCounts.Store(meta.Name, count)

				fmt.Printf("   [Telemetry] Node %q reported error (Failure %d/3)\n", meta.Name, count)

				if count == 3 {
					fmt.Println("\n🧬 [AST Mutator] Failure threshold reached. Synthesizing new topology...")

					newDSL := strings.Replace(currentDSL, meta.Name, "safe_fallback", 1)
					fmt.Printf("   • Old Topology: %s\n", currentDSL)
					fmt.Printf("   • New Topology: %s\n", newDSL)

					newBld, compileErr := flow.CompilePipeline(newDSL, registry)
					if compileErr != nil {
						panic(compileErr)
					}

					newBuilt := newBld.Build()
					_ = action.ApplyTreeHook(newBuilt, supervisorHook)

					// Atomic lock-free hot swap on active pointer
					livePipeline.Swap(newBuilt)

					currentDSL = newDSL

					failCounts.Store(meta.Name, int32(0))
					fmt.Println("✨ [Hot-Swap] Live execution pointer updated (0 dropped connections)")
				}
			} else if strings.Contains(currentDSL, meta.Name) {
				failCounts.Store(meta.Name, int32(0))
			}
		},
	}

	for _, a := range registry.Actions() {
		a.AddAnyHook(supervisorHook)
	}

	fmt.Printf("⚡ Initial Topology Deployed: %s\n", currentDSL)
	fmt.Println(strings.Repeat("─", 78))

	for i := 1; i <= 8; i++ {
		time.Sleep(40 * time.Millisecond)

		res, execErr := livePipeline.DoAny(ctx, WorkerReq{Data: "chunk_01"})
		if execErr != nil {
			fmt.Printf("➡️  Req #%d: ❌ [503 Error] Execution aborted by node failure\n", i)
		} else {
			r := res.(WorkerRes)
			fmt.Printf("➡️  Req #%d: ✅ [200 OK] Output: %s\n", i, r.Result)
		}
	}

	fmt.Println(strings.Repeat("─", 78))
	fmt.Printf("• Final Running Topology : %s\n", currentDSL)
	fmt.Println("• Resilience Status      : Recovered automatically without process restarts")
	fmt.Println(strings.Repeat("═", 78))
}
