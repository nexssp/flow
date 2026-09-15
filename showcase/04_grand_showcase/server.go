package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
	"github.com/nexssp/transportai/ta2a"
)

func BuildServerActions(proxy *action.Proxy) []action.AnyAction {
	endpoint := action.New(
		"pipeline.modernize",
		func(ctx context.Context, req MigrationTask) (ModernizationOutcome, error) {
			tenantCtx := WithTenantContext(ctx, req.Tenant)

			res, err := proxy.DoAny(tenantCtx, req)
			if err != nil {
				return ModernizationOutcome{}, err
			}

			outcome, ok := res.(ModernizationOutcome)
			if !ok {
				return ModernizationOutcome{}, fmt.Errorf("unexpected output type %T", res)
			}

			return outcome, nil
		},
	).
		Description("Concurrent multi-tenant modernization pipeline with total security envelope").
		Tag("pipeline", "modernization", "security", "a2a").
		Route(
			thttp.POST("/api/v1/modernize"),
			ta2a.Role("code-modernizer").WithDescription("Autonomous Multi-Tenant Modernization Server"),
		).
		Build()

	return []action.AnyAction{endpoint}
}

func StartA2ATransport(ctx context.Context, actions []action.AnyAction, port string) (*ta2a.Transport, error) {
	server := ta2a.New(port, nil,
		ta2a.WithAgentCard(ta2a.AgentCard{
			Name:        "nexss-enterprise-guard-modernizer",
			Description: "Concurrent Multi-Tenant Modernizer with Live Security, Explainability & Cost Governance",
			Version:     "2.0.0",
			Capabilities: map[string]bool{
				"multiTenant":      true,
				"costAccounting":   true,
				"promptFirewall":   true,
				"piiRedaction":     true,
				"zeroDowntimeSwap": true,
			},
			Skills: []string{"firewall", "planner", "architect", "sandbox", "audit", "eval-judge"},
		}),
	)

	server.Mount(actions)
	go func() { _, _ = server.Do(ctx, nil) }()

	time.Sleep(20 * time.Millisecond)

	return server, nil
}
