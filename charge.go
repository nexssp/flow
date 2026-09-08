package flow

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/flow/governance"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

func ChargeBranch(ctx context.Context, ledger CostLedger, g *CompiledGraph, event CostEvent) error {
	if ledger == nil {
		return nil
	}
	if g == nil {
		return fmt.Errorf("flow: compiled graph is required for budget verification")
	}

	if event.RunID == "" {
		event.RunID = action.ExecutionIDFrom(ctx)
	}
	if event.TenantID == "" {
		event.TenantID = xctx.TenantIDFrom(ctx)
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}

	checks := make([]governance.BudgetCheck, 0, 4)

	if limit := g.Budget.MaxCostMicrosPerRun; limit > 0 && event.RunID != "" {
		checks = append(checks, governance.BudgetCheck{
			Scope: governance.ScopeRun,
			Key:   event.RunID,
			Limit: limit,
		})
	}

	if limit := g.Budget.MaxCostMicrosPerDay; limit > 0 {
		checks = append(checks, governance.BudgetCheck{
			Scope: governance.ScopeDay,
			Key:   event.RecordedAt.Format("2006-01-02"),
			Limit: limit,
		})
	}

	if limit := g.Budget.MaxCostMicrosPerMonth; limit > 0 {
		checks = append(checks, governance.BudgetCheck{
			Scope: governance.ScopeMonth,
			Key:   event.RecordedAt.Format("2006-01"),
			Limit: limit,
		})
	}

	if limit := g.Budget.MaxCostMicrosPerTenant; limit > 0 && event.TenantID != "" {
		checks = append(checks, governance.BudgetCheck{
			Scope: governance.ScopeTenant,
			Key:   event.TenantID,
			Limit: limit,
		})
	}

	return ledger.Reserve(ctx, event, checks)
}
