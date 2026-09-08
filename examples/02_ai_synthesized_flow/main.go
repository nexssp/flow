package main

import (
	"context"
	"fmt"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

func main() {
	ctx := context.Background()

	fetchUser := action.New("user.get", func(_ context.Context, id int) (map[string]any, error) {
		return map[string]any{
			"user_id": id,
			"email":   "alice@nexss.com",
			"plan":    "enterprise",
		}, nil
	}).
		Description("Fetches user account by ID").
		Build()

	sendWelcome := action.New("email.welcome", func(_ context.Context, req map[string]any) (string, error) {
		return fmt.Sprintf("Welcome email sent to %s on plan %s", req["to"], req["plan"]), nil
	}).
		Description("Sends an onboarding email").
		Build()

	registry := flow.NewRegistry(fetchUser, sendWelcome)

	capabilities := flow.ExtractCapabilities(registry)
	fmt.Printf("🤖 AI Agent discovered %d capabilities:\n", len(capabilities))
	for _, cap := range capabilities {
		fmt.Printf("   - %-18s %s\n", cap.Name, cap.Description)
	}

	aiSynthesizedDSL := `user.get -> { to: email, plan: plan } -> email.welcome`
	fmt.Printf("\n✨ AI-synthesized pipeline:\n   %s\n\n", aiSynthesizedDSL)

	builder, err := flow.CompilePipeline(aiSynthesizedDSL, registry)
	if err != nil {
		panic(err)
	}

	res, err := builder.Build().Do(ctx, 101)
	if err != nil {
		panic(err)
	}

	fmt.Printf("✅ Dynamic agent result: %v\n", res)
}
