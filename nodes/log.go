// Log nodes. Input is a map so a log node can be inserted anywhere in a
// pipeline without a projection: the previous node's output is logged as
// structured attributes and passed through unchanged.
//
// Convention:
//
//	"message"  : string   — the log line (optional)
//	other keys : any      — structured attributes
//
// Zero allocation when the level is disabled: slog.Default().Enabled is
// checked before any work is done.
package nodes

import (
	"context"
	"log/slog"

	"github.com/nexssp/kernel/action"
)

const (
	LogInfoName  = "log.info"
	LogWarnName  = "log.warn"
	LogErrorName = "log.error"
)

func NewLogInfoAction() action.AnyAction  { return newLogNode(LogInfoName, slog.LevelInfo) }
func NewLogWarnAction() action.AnyAction  { return newLogNode(LogWarnName, slog.LevelWarn) }
func NewLogErrorAction() action.AnyAction { return newLogNode(LogErrorName, slog.LevelError) }

func newLogNode(name string, level slog.Level) action.AnyAction {
	return action.New(name, func(ctx context.Context, in map[string]any) (map[string]any, error) {
		logger := slog.Default()
		if !logger.Enabled(ctx, level) {
			return in, nil
		}

		msg, _ := in["message"].(string)

		attrs := make([]slog.Attr, 0, len(in))
		for k, v := range in {
			if k == "message" {
				continue
			}

			attrs = append(attrs, slog.Any(k, v))
		}

		if len(attrs) == 0 {
			logger.Log(ctx, level, msg)
		} else {
			logger.LogAttrs(ctx, level, msg, attrs...)
		}

		return in, nil
	}).
		Description("Emit a structured log line and pass the input through").
		Tag("log", "observe").
		Build()
}
