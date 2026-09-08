package governance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
)

type CostScope string

const (
	ScopeRun    CostScope = "run"
	ScopeDay    CostScope = "day"
	ScopeMonth  CostScope = "month"
	ScopeTenant CostScope = "tenant"
)

type CostUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CostMicros   int64 `json:"cost_micros"`
}

type CostEvent struct {
	RunID      string    `json:"run_id"`
	TenantID   string    `json:"tenant_id"`
	SourceNode string    `json:"source_node"`
	RecordedAt time.Time `json:"recorded_at"`
	Usage      CostUsage `json:"usage"`
}

type BudgetCheck struct {
	Scope CostScope
	Key   string
	Limit int64
}

type CostLedger interface {
	Reserve(ctx context.Context, event CostEvent, limits []BudgetCheck) error
	Events(ctx context.Context, runID string) ([]CostEvent, error)
}

type MemoryCostLedger struct {
	mu     sync.Mutex
	totals map[string]int64
	events map[string][]CostEvent
}

func NewMemoryCostLedger() *MemoryCostLedger {
	return &MemoryCostLedger{
		totals: make(map[string]int64),
		events: make(map[string][]CostEvent),
	}
}

func (l *MemoryCostLedger) Reserve(_ context.Context, event CostEvent, limits []BudgetCheck) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Usage.CostMicros < 0 {
		return xerr.BadRequest("governance: cost cannot be negative")
	}

	for _, check := range limits {
		if check.Limit <= 0 {
			continue
		}
		if l.totals[string(check.Scope)+"\x00"+check.Key]+event.Usage.CostMicros > check.Limit {
			return xerr.Forbidden(fmt.Sprintf("governance: cost budget exceeded for %s %q (limit: %d micros)", check.Scope, check.Key, check.Limit))
		}
	}

	for _, check := range limits {
		if check.Limit > 0 {
			l.totals[string(check.Scope)+"\x00"+check.Key] += event.Usage.CostMicros
		}
	}
	l.events[event.RunID] = append(l.events[event.RunID], event)
	return nil
}

func (l *MemoryCostLedger) Events(_ context.Context, runID string) ([]CostEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CostEvent(nil), l.events[runID]...), nil
}

func AsCostHook(ledger CostLedger, estimatedCostMicros int64, budgetLimitMicros int64) action.AnyHook {
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, meta *action.Meta) (context.Context, error) {
			if ledger == nil || estimatedCostMicros <= 0 {
				return ctx, nil
			}

			runID := action.ExecutionIDFrom(ctx)
			tenantID := xctx.TenantIDFrom(ctx)

			event := CostEvent{
				RunID:      runID,
				TenantID:   tenantID,
				SourceNode: meta.Name,
				Usage:      CostUsage{CostMicros: estimatedCostMicros},
				RecordedAt: time.Now().UTC(),
			}

			var limits []BudgetCheck
			if budgetLimitMicros > 0 {
				limits = append(limits, BudgetCheck{
					Scope: ScopeRun,
					Key:   runID,
					Limit: budgetLimitMicros,
				})
			}

			if err := ledger.Reserve(ctx, event, limits); err != nil {
				return ctx, err
			}
			return ctx, nil
		},
	}
}
