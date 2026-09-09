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

func main() {
	ctx := context.Background()

	var requestCount int32

	fetch := action.New("sys.fetch", func(_ context.Context, _ any) (string, error) {
		return "raw_data", nil
	}).Build()

	processFast := action.New("sys.process_fast", func(_ context.Context, req string) (string, error) {
		if atomic.AddInt32(&requestCount, 1) > 3 {
			return "", fmt.Errorf("connection refused: sys.process_fast is down")
		}
		return req + "_processed_fast", nil
	}).Build()

	processSafe := action.New("sys.process_safe", func(_ context.Context, req string) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return req + "_processed_safely", nil
	}).Build()

	save := action.New("sys.save", func(_ context.Context, req string) (string, error) {
		return "SAVED: " + req, nil
	}).Build()

	registry := flow.NewRegistry(fetch, processFast, processSafe, save)

	currentDSL := "sys.fetch -> sys.process_fast -> sys.save"
	initialPipeline, err := flow.CompilePipeline(currentDSL, registry)
	if err != nil {
		panic(err)
	}

	initialBuilt := initialPipeline.Build()
	livePipeline := action.NewProxy(initialBuilt)

	var failCounts sync.Map
	var nervousSystemHook action.AnyHook

	nervousSystemHook = action.AnyHook{
		After: func(_ context.Context, req, res any, err error, meta *action.Meta) {
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

				fmt.Printf("   [HOOK ALERT] Capability %q failed. (Strike %d)\n", meta.Name, count)

				if count == 3 {
					fmt.Printf("\n🧬 [BORG MUTATION TRIGGERED] Chronic failure on %q.\n", meta.Name)

					newDSL := strings.Replace(currentDSL, meta.Name, "sys.process_safe", 1)
					fmt.Printf("🧬 [BORG COMPILING] Rewriting AST...\n   OLD: %s\n   NEW: %s\n", currentDSL, newDSL)

					newPipeline, compileErr := flow.CompilePipeline(newDSL, registry)
					if compileErr != nil {
						panic("Borg mutation failed to compile: " + compileErr.Error())
					}

					newBuilt := newPipeline.Build()
					_ = action.ApplyTreeHook(newBuilt, nervousSystemHook)

					livePipeline.Swap(newBuilt)
					currentDSL = newDSL
					failCounts.Store(meta.Name, int32(0))

					fmt.Printf("🧬 [BORG HOT-SWAP COMPLETE] Live traffic shifted to new topology.\n\n")
				}
			} else if strings.Contains(currentDSL, meta.Name) {
				failCounts.Store(meta.Name, int32(0))
			}
		},
	}

	for _, act := range registry.Actions() {
		act.AddAnyHook(nervousSystemHook)
	}

	fmt.Println("🚀 Starting Traffic Simulation (10 Requests)")
	fmt.Printf("Initial Architecture: %s\n", currentDSL)
	fmt.Println(strings.Repeat("-", 70))

	for i := 1; i <= 10; i++ {
		time.Sleep(100 * time.Millisecond)

		fmt.Printf("➡️  Req #%d: ", i)
		res, err := livePipeline.DoAny(ctx, nil)

		if err != nil {
			fmt.Printf("❌ FAILED Pipeline execution stopped.\n")
		} else {
			fmt.Printf("✅ SUCCESS: %v\n", res)
		}
	}

	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("🏁 Final Architecture Running in Memory: %s\n", currentDSL)
}
