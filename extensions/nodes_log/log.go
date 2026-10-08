package nodes_log

import (
	"context"
	"log/slog"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/redact"
)

// LogInfo, LogWarn, LogError emit a structured line at the matching
// slog level and return the input unchanged. The message field, when
// present, becomes the slog message; every other field becomes an
// attribute after passing through redact.Map, which masks any field
// whose name looks like a credential. The input map itself is never
// mutated — the same value flows downstream unchanged.
var (
	LogInfo  = newLogNode("log.info", slog.LevelInfo)
	LogWarn  = newLogNode("log.warn", slog.LevelWarn)
	LogError = newLogNode("log.error", slog.LevelError)
)

func newLogNode(name string, level slog.Level) action.AnyAction {
	return action.New(name, func(ctx context.Context, in map[string]any) (map[string]any, error) {
		logger := slog.Default()
		if !logger.Enabled(ctx, level) {
			return in, nil
		}

		message, _ := in["message"].(string)

		safe := redact.Map(in)
		attributes := make([]slog.Attr, 0, len(safe))
		for key, value := range safe {
			if key == "message" {
				continue
			}
			attributes = append(attributes, slog.Any(key, value))
		}

		if len(attributes) == 0 {
			logger.Log(ctx, level, message)
		} else {
			logger.LogAttrs(ctx, level, message, attributes...)
		}
		return in, nil
	}).
		Description("Emit a structured log line and pass the input through").
		Tag("log", "observe").
		Build()
}
