package modifiers_core

import (
	"time"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

// Modifiers returns the Kernel-owned policy modifiers. Every entry is
// tagged OwnerKernel so NewModifierTable permits registration only from
// this bundle.
func Modifiers() []compiler.Modifier {
	return []compiler.Modifier{
		kernel(compiler.WithInheritable(compiler.WithUnique(
			compiler.Duration("timeout", (*action.Builder[any, any]).Timeout),
		))),

		kernel(compiler.WithInheritable(
			compiler.Int("retry", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
				return b.Retry(n, action.ExponentialJitter(100*time.Millisecond, 30*time.Second))
			}),
		)),

		kernel(compiler.WithInheritable(compiler.WithUnique(
			compiler.Int32("concurrency", (*action.Builder[any, any]).ConcurrencyLimit),
		))),

		kernel(compiler.WithInheritable(compiler.WithUnique(
			compiler.Duration("cache", func(b *action.Builder[any, any], ttl time.Duration) *action.Builder[any, any] {
				return b.Cache(ttl, defaultKey)
			}),
		))),

		kernel(compiler.WithInheritable(
			compiler.Flag("coalesce", func(b *action.Builder[any, any]) *action.Builder[any, any] {
				return b.Coalesce(action.NewCoalescer(), defaultKey)
			}),
		)),

		kernel(compiler.WithInheritable(
			compiler.Flag("dedup", func(b *action.Builder[any, any]) *action.Builder[any, any] {
				return b.Dedup(defaultKey)
			}),
		)),

		kernel(compiler.WithInheritable(
			compiler.Flag("idempotent", (*action.Builder[any, any]).Idempotent),
		)),

		kernel(compiler.WithInheritable(compiler.WithUnique(
			compiler.Int("rate_limit", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
				return b.RateLimit(float64(n), n)
			}),
		))),
	}
}

// kernel marks a modifier as OwnerKernel.
func kernel(m compiler.Modifier) compiler.Modifier {
	return compiler.WithOwner(m, compiler.OwnerKernel)
}
