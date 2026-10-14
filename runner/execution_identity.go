package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

var (
	durableCountersContextKey = xctx.NewKey[*durableStepCounters]("flow.runner.durable-step-counters")
	stepIdentityContextKey    = xctx.NewKey[string]("flow.runner.step-identity")
)

type durableStepCounters struct {
	mu     sync.Mutex
	counts map[string]uint64
}

func (c *durableStepCounters) next(stepName string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = make(map[string]uint64)
	}
	ordinal := c.counts[stepName]
	c.counts[stepName] = ordinal + 1
	return ordinal
}

// ensureExecutionIdentity preserves a caller-supplied Kernel execution ID or
// creates one for this invocation. The per-run occurrence counters make a
// repeated static callsite (for example, inside LoopAny) produce distinct but
// repeatable durable step names on a retry.
func ensureExecutionIdentity(ctx context.Context) (context.Context, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := xctx.ExecutionIDFrom(ctx)
	if id == "" {
		id = xctx.RootExecutionIDFrom(ctx)
	}
	if id == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, "", xerr.Internal("generate execution ID", err)
		}
		id = "flow-" + hex.EncodeToString(raw[:])
	}
	ctx = xctx.WithExecutionID(ctx, id)
	ctx = durableCountersContextKey.With(ctx, &durableStepCounters{})
	return ctx, id, nil
}

func runnerAtomAdvisor(journal action.Journal, idempotencyStore action.IdempotencyStore) compiler.AtomAdviseFunc {
	return func(atom *compiler.Atom, builder *action.Builder[any, any]) error {
		var (
			retained        []string
			retainedSources []compiler.ModifierSource
			hasIdempotent   bool
		)
		if idempotencyStore != nil {
			for i, raw := range atom.Modifiers {
				if raw == "idempotent" {
					hasIdempotent = true
					continue
				}
				retained = append(retained, raw)
				if i < len(atom.ModifierSources) {
					retainedSources = append(retainedSources, atom.ModifierSources[i])
				}
			}
		}

		if journal == nil && !hasIdempotent {
			return nil
		}
		if atom.Pos.Line <= 0 || atom.Pos.Col <= 0 {
			return compiler.SourceError(atom.Pos, "runner-managed execution needs a source-positioned action call")
		}
		staticStepName := fmt.Sprintf("%s|%s", atom.Pos.String(), atom.Name)

		if hasIdempotent {
			builder.IdempotentWithConfig(action.IdempotencyConfig{
				Enabled: true,
				Store:   newScopedIdempotencyStore(idempotencyStore, staticStepName),
			})
			atom.Modifiers = retained
			atom.ModifierSources = retainedSources
		}
		// The step middleware must run outside both Durable and
		// IdempotencyMiddleware so its per-occurrence identity is visible to
		// the journal and scoped idempotency store.
		builder.UseFirst(runnerStepMiddleware(journal, staticStepName))
		return nil
	}
}

func runnerStepMiddleware(journal action.Journal, staticStepName string) action.Middleware[any, any] {
	return func(next action.Fn[any, any]) action.Fn[any, any] {
		var (
			mu       sync.Mutex
			handlers = make(map[uint64]action.Fn[any, any])
		)
		return func(ctx context.Context, req any) (any, error) {
			counters, _ := durableCountersContextKey.From(ctx)
			if counters == nil {
				counters = &durableStepCounters{}
			}
			ordinal := counters.next(staticStepName)
			stepName := fmt.Sprintf("%s#%d", staticStepName, ordinal)
			stepCtx := stepIdentityContextKey.With(ctx, stepName)
			if journal == nil {
				return next(stepCtx, req)
			}

			mu.Lock()
			handler := handlers[ordinal]
			if handler == nil {
				handler = action.Durable[any, any](journal, stepName)(next)
				handlers[ordinal] = handler
			}
			mu.Unlock()
			return handler(stepCtx, req)
		}
	}
}
