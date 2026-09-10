package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nexssp/cost"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type tenantCtxKey struct{}

func WithTenantContext(ctx context.Context, t TenantContext) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, t)
}

func TenantFromContext(ctx context.Context) (TenantContext, bool) {
	if ctx == nil {
		return TenantContext{}, false
	}

	t, ok := ctx.Value(tenantCtxKey{}).(TenantContext)

	return t, ok
}

type TenantLedgerRegistry struct {
	mu      sync.RWMutex
	ledgers map[string]*cost.Ledger
}

func NewTenantLedgerRegistry() *TenantLedgerRegistry {
	return &TenantLedgerRegistry{
		ledgers: make(map[string]*cost.Ledger),
	}
}

func (r *TenantLedgerRegistry) ProvisionTenant(tenantID string, budgetMicros int64) *cost.Ledger {
	r.mu.Lock()
	defer r.mu.Unlock()

	l := cost.NewLedger(budgetMicros, cost.USD)
	r.ledgers[tenantID] = l

	return l
}

func (r *TenantLedgerRegistry) Get(tenantID string) (*cost.Ledger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	l, ok := r.ledgers[tenantID]

	return l, ok
}

type tenantReservationKey struct{}

func TenantCostHook(reg *TenantLedgerRegistry, estimateMicros int64) action.AnyHook {
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
			tenant, ok := TenantFromContext(ctx)
			if !ok || tenant.TenantID == "" {
				return ctx, xerr.Unauthorized("tenant context missing from boundary")
			}

			ledger, exists := reg.Get(tenant.TenantID)
			if !exists {
				return ctx, xerr.Forbidden(fmt.Sprintf("tenant %q has no ledger", tenant.TenantID))
			}

			reservation, err := ledger.Reserve(ctx, estimateMicros)
			if err != nil {
				return ctx, fmt.Errorf("budget exceeded for tenant %s: %w", tenant.TenantID, err)
			}

			return context.WithValue(ctx, tenantReservationKey{}, reservation), nil
		},

		After: func(ctx context.Context, _ any, result any, actionErr error, meta *action.Meta) {
			reservation, ok := ctx.Value(tenantReservationKey{}).(cost.Reservation)
			if !ok || reservation == nil {
				return
			}

			tenant, _ := TenantFromContext(ctx)
			ledger, _ := reg.Get(tenant.TenantID)

			cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()

			if actionErr != nil {
				_ = reservation.Release(cleanCtx)

				return
			}

			actual := estimateMicros
			if reporter, ok := result.(cost.CostReporter); ok {
				actual = reporter.CostMicros()
				if actual < 0 {
					actual = 0
				}
			}

			if err := reservation.Commit(cleanCtx, actual); err == nil && actual > 0 && ledger != nil {
				_ = ledger.Record(cleanCtx, cost.Event{
					ID:         fmt.Sprintf("evt_%d", time.Now().UnixNano()),
					Domain:     tenant.TenantID,
					Operation:  meta.Name,
					CostMicros: actual,
					Currency:   cost.USD,
					Timestamp:  time.Now().UTC(),
				})
			}
		},
	}
}
