package runtime

import (
	"context"
	"time"

	"github.com/nexssp/kernel/action"
)

// Sleep pauses execution for the specified milliseconds.
// It respects context cancellation, so if a pipeline times out or
// is canceled, the sleep aborts immediately instead of holding the goroutine.
var Sleep = action.New("sleep", func(ctx context.Context, in any) (any, error) {
	ms := 1000 // default 1 second
	if m, ok := in.(map[string]any); ok {
		if val, exists := m["duration_ms"]; exists {
			switch v := val.(type) {
			case float64:
				ms = int(v)
			case int:
				ms = v
			}
		}
	}

	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return in, nil
}).Description("Pause execution for duration_ms (default 1000ms) and return input").
	Tag("base", "simulate").
	Build()
