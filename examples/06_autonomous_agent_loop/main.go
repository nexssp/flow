package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx := context.Background()

	plan := action.New("agent.planner", func(_ context.Context, goal string) (map[string]any, error) {
		fmt.Printf("🎯 [PLANNER] Goal: %s\n", goal)
		return map[string]any{
			"goal":      goal,
			"turns":     0,
			"completed": false,
		}, nil
	}).Build()

	reason := action.New("agent.reason", func(_ context.Context, state map[string]any) (map[string]any, error) {
		turn := 0
		if t, ok := state["turns"].(int); ok {
			turn = t + 1
		}

		fmt.Printf("💭 [MANUS TURN %d] Thinking and choosing tool...\n", turn)
		time.Sleep(25 * time.Millisecond)

		completed := turn >= 3
		output := fmt.Sprintf("Turn %d output: sandbox test executed successfully", turn)
		if completed {
			output = "SUCCESS: service built, tested and deployed to staging"
		}

		return map[string]any{
			"goal":      state["goal"],
			"completed": completed,
			"turns":     turn,
			"output":    output,
		}, nil
	}).Build()

	webSearch := action.New("tool.web_search", func(_ context.Context, state map[string]any) (map[string]any, error) {
		return map[string]any{
			"goal":      state["goal"],
			"turns":     state["turns"],
			"completed": false,
			"output":    "Search result: use production-grade retries and structured logging",
		}, nil
	}).Build()

	summarize := action.New("agent.summarize", func(_ context.Context, state map[string]any) (string, error) {
		return fmt.Sprintf(
			"🎉 [MANUS OS FINAL]\nGoal status: completed=%v\nTurns used: %v\nDetails: %v",
			state["completed"],
			state["turns"],
			state["output"],
		), nil
	}).Build()

	registry := flow.NewRegistry(plan, reason, webSearch, summarize)

	dsl := `
		agent.planner
		-> loop( agent.reason || tool.web_search ) until( completed == true )
		-> { completed: completed, turns: turns, output: output }
		-> agent.summarize
	`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	start := time.Now()
	result, err := builder.Build().Do(ctx, "Build a zero-trust API and deploy it safely to staging")
	if err != nil {
		panic(err)
	}

	fmt.Println("----------------------------------------------------------------")
	fmt.Println(result)
	fmt.Printf("⚡ Total agent execution latency: %v\n", time.Since(start))
	fmt.Println("----------------------------------------------------------------")
}
