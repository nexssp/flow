package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx := context.Background()

	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("[nexssp/flow] Architecture 01: Dynamic State Projection Pipeline")
	fmt.Println(strings.Repeat("═", 78))
	fmt.Println("• Pattern   : Dynamic schema shaping via inline bytecode projections { ... }")
	fmt.Println("• Use-Case  : Fast prototyping, heterogeneous payload translation, API gateways")
	fmt.Println(strings.Repeat("─", 78))

	fetch := action.New("github.issue", func(_ context.Context, id int) (map[string]any, error) {
		fmt.Printf("   [1/3] github.issue   | Ingestion: Incident #%d retrieved from API\n", id)

		return map[string]any{
			"id":    id,
			"title": "Latency regression in auth service",
			"body":  "p99 exceeded 250ms threshold after deployment",
		}, nil
	}).Build()

	triage := action.New("ai.triage", func(_ context.Context, req map[string]any) (map[string]any, error) {
		fmt.Printf("   [2/3] ai.triage      | Evaluation: Prompt processed -> Severity: P1\n")

		return map[string]any{
			"urgency": "P1",
			"summary": fmt.Sprintf("High Urgency: %s", req["prompt"]),
		}, nil
	}).Build()

	notify := action.New("slack.notify", func(_ context.Context, req map[string]any) (string, error) {
		fmt.Printf("   [3/3] slack.notify   | Dispatch: Webhook sent -> Channel: #incidents\n")

		return fmt.Sprintf("Alert [%s]: %s", req["level"], req["message"]), nil
	}).Build()

	registry := flow.NewRegistry(fetch, triage, notify)

	dsl := `
		github.issue
		-> { prompt: title + " - " + body }
		-> ai.triage
		-> { level: urgency, message: summary }
		-> slack.notify
	`

	fmt.Printf("⚡ Compiling Arrow DSL Pipeline:\n%s\n\n", strings.TrimSpace(dsl))

	compileStart := time.Now()

	pipeline, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	fmt.Printf("⏱️  AST JIT Compilation : %v\n\n", time.Since(compileStart))

	execStart := time.Now()

	res, err := pipeline.Build().Do(ctx, 1042)
	if err != nil {
		panic(err)
	}

	fmt.Println(strings.Repeat("─", 78))
	fmt.Printf("• Result  : %v\n", res)
	fmt.Printf("• Latency : %v\n", time.Since(execStart))
	fmt.Println(strings.Repeat("═", 78))
}
