package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// We call cancel manually on all exit paths to avoid deferred cancel not running with os.Exit.
	// No defer cancel() here.

	getIssue := action.New("github.issue", func(_ context.Context, id int) (map[string]any, error) {
		if id <= 0 {
			return nil, xerr.BadRequest(fmt.Sprintf("invalid issue id: %d", id))
		}
		return map[string]any{
			"issue": map[string]any{
				"id":    id,
				"title": "Checkout API latency doubled after payment deploy",
				"body":  "Latency p99 went from 110ms to 420ms. Customers are seeing timeouts.",
			},
		}, nil
	}).Build()

	aiTriage := action.New("ai.triage", func(_ context.Context, req map[string]any) (map[string]any, error) {
		prompt, ok := req["prompt"].(string)
		if !ok || prompt == "" {
			return nil, xerr.BadRequest("ai.triage: prompt string parameter is required")
		}
		fmt.Printf("🧠 AI Triage received:\n   %s\n", prompt)

		return map[string]any{
			"severity":         "SEV1",
			"affected_service": "checkout-api",
			"oncall":           "payments-oncall",
			"likely_cause":     "recent payment deploy",
		}, nil
	}).Build()

	pageOncall := action.New("pagerduty.trigger", func(_ context.Context, req map[string]any) (string, error) {
		team, _ := req["team"].(string)
		service, _ := req["service"].(string)
		severity, _ := req["severity"].(string)

		if team == "" || service == "" {
			return "", xerr.BadRequest("pagerduty.trigger: team and service are required parameters")
		}

		return fmt.Sprintf(
			"Paged %s for %s (%s)",
			team,
			service,
			severity,
		), nil
	}).Build()

	registry := flow.NewRegistry(getIssue, aiTriage, pageOncall)

	dsl := `
		github.issue
		-> { prompt: "Triage this incident: " + issue.title + ". Body: " + issue.body }
		-> ai.triage
		-> { team: oncall, service: affected_service, severity: severity }
		-> pagerduty.trigger
	`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Pipeline compilation failed: %v\n", err)
		cancel()
		os.Exit(1)
	}

	result, err := builder.Build().Do(ctx, 1042)
	if err != nil {
		handleRuntimeError(err)
		cancel()
		os.Exit(1)
	}

	cancel()
	fmt.Printf("✅ Incident response: %v\n", result)
}

func handleRuntimeError(err error) {
	var appErr *xerr.AppError
	if errors.As(err, &appErr) {
		fmt.Fprintf(os.Stderr, "❌ Validation/Request Error: %v (Kind: %v)\n", appErr.Message, appErr.Kind)
		return
	}
	fmt.Fprintf(os.Stderr, "❌ Workflow execution failure: %v\n", err)
}
