package dslparse

import "time"

// Cache and dedup modifiers.
//
//	:cache=, :cache_key=, :coalesce, :dedup, :idempotent, :idempotency_header=
func init() {
	registerMod("cache", func(p *Modifiers, v string, _ bool) error {
		d, err := time.ParseDuration(trimValue(v))
		if err != nil {
			return err
		}
		p.CacheTTL = d
		return nil
	})

	registerMod("cache_key", func(p *Modifiers, v string, _ bool) error {
		p.CacheKey = trimValue(v)
		return nil
	})

	registerMod("coalesce", func(p *Modifiers, _ string, _ bool) error {
		// Marked in Modifiers via a boolean later; for now no-op is
		// acceptable because the compiler reads raw modifiers for
		// booleans. This entry exists so a future cleanup can move the
		// boolean to a struct field without touching the parser.
		return nil
	})

	registerMod("dedup", func(p *Modifiers, _ string, _ bool) error {
		return nil
	})

	registerMod("idempotent", func(p *Modifiers, _ string, _ bool) error {
		p.Idempotent = true
		return nil
	})

	registerMod("idempotency_header", func(p *Modifiers, v string, _ bool) error {
		p.IdempotencyHeader = trimValue(v)
		return nil
	})
}
