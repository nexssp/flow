package nodes_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow/nodes"
)

type sampleUser struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
	Age  int    `json:"age"`
}

type NestedPayload struct {
	TicketID    string `json:"ticket_id"`
	SessionUUID string `json:"session_uuid"`
	StatusCode  int    `json:"status_code"`
}

func TestProjection_StandardSyntax(t *testing.T) {
	t.Parallel()

	proj, err := nodes.NewProjectionAction(`{ welcome: "Hello " + name, is_adult: age >= 18 }`)
	if err != nil {
		t.Fatalf("NewProjectionAction failed: %v", err)
	}

	input := map[string]any{"name": "Alice", "age": 25}

	out, err := proj.Do(context.Background(), input)
	if err != nil {
		t.Fatalf("projection execution failed: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}

	if res["welcome"] != "Hello Alice" {
		t.Errorf("expected 'Hello Alice', got %v", res["welcome"])
	}

	if res["is_adult"] != true {
		t.Errorf("expected is_adult=true, got %v", res["is_adult"])
	}
}

func TestProjection_JQSyntaxPrefix(t *testing.T) {
	t.Parallel()

	proj, err := nodes.NewProjectionAction(`{ to: .name, account_tier: .tier }`)
	if err != nil {
		t.Fatalf("NewProjectionAction failed: %v", err)
	}

	input := sampleUser{Name: "Bob", Tier: "pro", Age: 30}

	out, err := proj.Do(context.Background(), input)
	if err != nil {
		t.Fatalf("projection execution failed: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}

	if res["to"] != "Bob" || res["account_tier"] != "pro" {
		t.Errorf("unexpected output: %+v", res)
	}
}

func TestProjection_StandaloneRootDot(t *testing.T) {
	t.Parallel()

	proj, err := nodes.NewProjectionAction(`{ result: . }`)
	if err != nil {
		t.Fatalf("NewProjectionAction failed: %v", err)
	}

	out, err := proj.Do(context.Background(), 42)
	if err != nil {
		t.Fatalf("projection execution failed: %v", err)
	}

	res, ok := out.(map[string]any)
	if !ok || res["result"] != 42 {
		t.Errorf("expected map[result:42], got %+v", out)
	}
}

// TestProjection_CasingAgnosticMultiStyle verifies that PascalCase, snake_case,
// and lowercase all resolve to the exact same value in both top-level and nested struct contexts.
func TestProjection_CasingAgnosticMultiStyle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("Top-Level Struct Field Variants", func(t *testing.T) {
		proj, err := nodes.NewProjectionAction(`{
			from_pascal: TicketID,
			from_snake:  ticket_id,
			from_lower:  ticketid
		}`)
		if err != nil {
			t.Fatalf("compile projection failed: %v", err)
		}

		input := NestedPayload{
			TicketID:    "TICKET-999",
			SessionUUID: "UUID-001",
			StatusCode:  200,
		}

		out, err := proj.Do(ctx, input)
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", out)
		}

		if m["from_pascal"] != "TICKET-999" || m["from_snake"] != "TICKET-999" || m["from_lower"] != "TICKET-999" {
			t.Fatalf("casing mismatch at top-level: %+v", m)
		}
	})

	t.Run("Nested Struct Inside Parallel Map Variants", func(t *testing.T) {
		// Simulates output from: ( sandbox_runner & sec_auditor )
		proj, err := nodes.NewProjectionAction(`{
			pascal_access: runner.TicketID,
			snake_access:  runner.ticket_id,
			lower_access:  runner.ticketid
		}`)
		if err != nil {
			t.Fatalf("compile projection failed: %v", err)
		}

		input := map[string]any{
			"runner": NestedPayload{
				TicketID:    "TICKET-NESTED-777",
				SessionUUID: "UUID-002",
				StatusCode:  200,
			},
		}

		out, err := proj.Do(ctx, input)
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", out)
		}

		if m["pascal_access"] != "TICKET-NESTED-777" ||
			m["snake_access"] != "TICKET-NESTED-777" ||
			m["lower_access"] != "TICKET-NESTED-777" {
			t.Fatalf("casing mismatch in nested map struct: %+v", m)
		}
	})
}
