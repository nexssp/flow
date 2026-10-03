package modifiers_core

import (
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// Modifiers returns the Kernel-owned policy modifiers. Every entry is
// tagged OwnerKernel so NewModifierTable permits registration only from
// this bundle.
func Modifiers() []core.Modifier {
	return []core.Modifier{
		kernel(core.WithInheritable(core.WithUnique(
			core.Duration("timeout", (*action.Builder[any, any]).Timeout),
		))),

		kernel(core.WithInheritable(
			core.Int("retry", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
				return b.Retry(n, action.ExponentialJitter(100*time.Millisecond, 30*time.Second))
			}),
		)),

		kernel(core.WithInheritable(core.WithUnique(
			core.Int32("concurrency", (*action.Builder[any, any]).ConcurrencyLimit),
		))),

		kernel(core.WithInheritable(core.WithUnique(
			core.Duration("cache", func(b *action.Builder[any, any], ttl time.Duration) *action.Builder[any, any] {
				return b.Cache(ttl, defaultKey)
			}),
		))),

		kernel(core.WithInheritable(
			core.Flag("coalesce", func(b *action.Builder[any, any]) *action.Builder[any, any] {
				return b.Coalesce(action.NewCoalescer(), defaultKey)
			}),
		)),

		kernel(core.WithInheritable(
			core.Flag("dedup", func(b *action.Builder[any, any]) *action.Builder[any, any] {
				return b.Dedup(defaultKey)
			}),
		)),

		kernel(core.WithInheritable(
			core.Flag("idempotent", (*action.Builder[any, any]).Idempotent),
		)),

		kernel(core.WithInheritable(core.WithUnique(
			core.Int("rate_limit", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
				return b.RateLimit(float64(n), n)
			}),
		))),
	}
}

// kernel marks a modifier as OwnerKernel.
func kernel(m core.Modifier) core.Modifier {
	return core.WithOwner(m, core.OwnerKernel)
}
