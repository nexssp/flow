package main

import (
	"context"
	"fmt"

	"github.com/nexssp/kernel/action"
)

type httpRouteBinding struct {
	Method string
	Path   string
}

func (r httpRouteBinding) HTTPRoute() (string, string) { return r.Method, r.Path }

type a2aRoleBinding struct {
	Role string
	Desc string
}

func (r a2aRoleBinding) WithDescription(d string) a2aRoleBinding {
	r.Desc = d
	return r
}

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
			httpRouteBinding{Method: "POST", Path: "/api/v1/modernize"},
			a2aRoleBinding{Role: "code-modernizer"}.WithDescription("Autonomous Multi-Tenant Modernization Server"),
		).
		Build()

	return []action.AnyAction{endpoint}
}

func StartA2ATransport(ctx context.Context, actions []action.AnyAction, port string) (any, error) {
	// Wewnątrz flow transport A2A jest symulowany lub podpinany przez trigger
	_ = ctx
	_ = actions
	_ = port
	return nil, nil
}
