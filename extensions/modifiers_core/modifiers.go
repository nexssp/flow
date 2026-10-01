package modifiers_core

import (
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

func Modifiers() []core.Modifier {
	return []core.Modifier{
		core.Duration("timeout", (*action.Builder[any, any]).Timeout),

		core.Int("retry", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
			return b.Retry(n, action.ExponentialJitter(100*time.Millisecond, 30*time.Second))
		}),

		core.Int32("concurrency", (*action.Builder[any, any]).ConcurrencyLimit),

		core.Duration("cache", func(b *action.Builder[any, any], ttl time.Duration) *action.Builder[any, any] {
			return b.Cache(ttl, defaultKey)
		}),

		core.Flag("coalesce", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Coalesce(action.NewCoalescer(), defaultKey)
		}),

		core.Flag("dedup", func(b *action.Builder[any, any]) *action.Builder[any, any] {
			return b.Dedup(defaultKey)
		}),

		core.Flag("idempotent", (*action.Builder[any, any]).Idempotent),

		core.Int("rate_limit", func(b *action.Builder[any, any], n int) *action.Builder[any, any] {
			return b.RateLimit(float64(n), n)
		}),
	}
}
