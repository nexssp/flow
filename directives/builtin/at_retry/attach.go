package at_retry

import (
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

func applyRetryPolicy(builder *action.Builder[any, any], decl Decl) {
	backoff := buildBackoff(decl)
	if len(decl.Only) > 0 {
		builder.RetryIf(decl.MaxAttempts, backoff, kindPredicate(decl.Only))
		return
	}
	builder.Retry(decl.MaxAttempts, backoff)
}

func buildBackoff(decl Decl) func(int) time.Duration {
	base := decl.BackoffBase
	maxD := decl.BackoffMax

	switch decl.BackoffKind {
	case "constant":
		if base <= 0 {
			base = time.Second
		}
		return action.ConstantBackoff(base)

	case "linear":
		if base <= 0 {
			base = 100 * time.Millisecond
		}
		return action.LinearBackoff(base)

	default: // "exponential" or empty
		if base <= 0 {
			base = 100 * time.Millisecond
		}
		if maxD <= 0 {
			maxD = 30 * time.Second
		}
		if decl.Jitter {
			return action.ExponentialJitter(base, maxD)
		}
		return action.ExponentialBackoff(base, maxD)
	}
}

func kindPredicate(kinds []string) action.RetryPredicate {
	set := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		set[k] = struct{}{}
	}
	return func(err error) bool {
		if err == nil {
			return false
		}
		_, ok := set[string(xerr.KindFrom(err))]
		return ok
	}
}
