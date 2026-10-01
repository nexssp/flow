package nodes_log

import (
	"context"
	"log/slog"

	"github.com/nexssp/kernel/action"
)

// LogInfo, LogWarn, LogError emit a structured line at the matching
// slog level and return the input unchanged. The message field, when
// present, becomes the slog message; every other field becomes an
// attribute.
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
		attributes := make([]slog.Attr, 0, len(in))
		for key, value := range in {
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
