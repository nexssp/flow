package retry

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

// advise scans the atom's modifiers for `:retry=N`, installs the retry
// middleware, and returns the modifier list without the consumed
// entries. Called by the AtomAdvise hook before the modifier table
// runs, so consumed modifiers are never applied twice.
func advise(cfg Config, atom *core.Atom, builder *action.Builder[any, any]) error {
	if atom == nil || builder == nil {
		return nil
	}
	remaining := atom.Modifiers[:0]
	for _, raw := range atom.Modifiers {
		name, value := splitModifier(raw)
		if name != "retry" {
			remaining = append(remaining, raw)
			continue
		}

		maxAttempts := cfg.DefaultMax
		if value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("retry expects a positive attempt count, got %q", value)
			}
			maxAttempts = parsed
		}

		backoff := action.ExponentialJitter(cfg.MinBackoff, cfg.MaxBackoff)
		if cfg.RetryAll {
			builder.RetryAll(maxAttempts, backoff)
		} else {
			builder.Retry(maxAttempts, backoff)
		}
	}
	atom.Modifiers = remaining
	return nil
}

func splitModifier(raw string) (name, value string) {
	if before, after, ok := strings.Cut(raw, "="); ok {
		return before, after
	}
	return raw, ""
}
