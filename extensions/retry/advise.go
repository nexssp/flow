package retry

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

// advise scans the atom's modifiers for `:retry=N`, installs retry
// middleware for positive values, and consumes zero as an explicit
// disable. It returns the modifier list without consumed entries. Called
// by the AtomAdvise hook before the modifier table runs, so consumed
// modifiers are never applied twice.
func advise(cfg Config, atom *compiler.Atom, builder *action.Builder[any, any]) error {
	if atom == nil || builder == nil {
		return nil
	}
	remaining := make([]string, 0, len(atom.Modifiers))
	var remainingValues []compiler.ModifierValue
	if atom.ModifierValues != nil {
		remainingValues = make([]compiler.ModifierValue, 0, len(atom.ModifierValues))
	}
	var remainingSources []compiler.ModifierSource
	if atom.ModifierSources != nil {
		remainingSources = make([]compiler.ModifierSource, 0, len(atom.ModifierSources))
	}
	for i, raw := range atom.Modifiers {
		name, value := splitModifier(raw)
		if name != "retry" {
			remaining = append(remaining, raw)
			if i < len(atom.ModifierValues) && remainingValues != nil {
				remainingValues = append(remainingValues, atom.ModifierValues[i])
			}
			if i < len(atom.ModifierSources) && remainingSources != nil {
				remainingSources = append(remainingSources, atom.ModifierSources[i])
			}
			continue
		}

		maxRetries := cfg.DefaultMax
		if value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				return fmt.Errorf("retry expects a positive attempt count, got %q", value)
			}
			if parsed == 0 {
				continue
			}
			maxRetries = parsed
		}

		backoff := cfg.backoffFunc()
		switch {
		case cfg.RetryAll:
			builder.RetryAll(maxRetries, backoff)
		case cfg.RetryPredicate != nil:
			builder.RetryIf(maxRetries, backoff, cfg.RetryPredicate)
		default:
			builder.Retry(maxRetries, backoff)
		}
	}
	atom.Modifiers = remaining
	atom.ModifierValues = remainingValues
	atom.ModifierSources = remainingSources
	return nil
}

func splitModifier(raw string) (name, value string) {
	if before, after, ok := strings.Cut(raw, "="); ok {
		return before, after
	}
	return raw, ""
}
