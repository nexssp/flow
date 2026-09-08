package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

type SwarmMessage struct {
	Query  string `json:"query"`
	Report string `json:"report,omitempty"`
	Output string `json:"output,omitempty"`
}

func main() {
	ctx := context.Background()

	// 1. Define local mock action adapters that represent remote A2A Agents.
	// In production, these use: client.AsAction("agent.researcher", "researcher")
	researcher := action.New("agent.researcher", func(_ context.Context, msg SwarmMessage) (SwarmMessage, error) {
		fmt.Printf("🕵️ [A2A Researcher] Finding data for: %s\n", msg.Query)
		msg.Report = "Security logs show unauthorized API key rotation on edge-node-03."
		return msg, nil
	}).Build()

	analyst := action.New("agent.analyst", func(_ context.Context, msg SwarmMessage) (SwarmMessage, error) {
		fmt.Printf("📊 [A2A Analyst] Correlating report metrics...\n")
		msg.Output = fmt.Sprintf("ANALYSIS COMPLETED: High risk identified. %s", msg.Report)
		return msg, nil
	}).Build()

	slackPoster := action.New("slack.dispatch", func(_ context.Context, msg SwarmMessage) (string, error) {
		fmt.Printf("💬 [Slack Outbound] Posting analysis to #security-alerts...\n")
		return fmt.Sprintf("Posted: %s", msg.Output), nil
	}).Build()

	registry := flow.NewRegistry(researcher, analyst, slackPoster)

	// 2. Chaining remote nodes seamlessly via standard Pipe syntax.
	dsl := `agent.researcher -> agent.analyst -> slack.dispatch`

	builder, err := flow.CompilePipeline(dsl, registry)
	if err != nil {
		panic(err)
	}

	result, err := builder.Build().Do(ctx, SwarmMessage{Query: "Investigate unusual key activities on edge"})
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n✅ Swarm execution response: %v\n", result)
}
