package on_error

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/contracts"
)

// ErrorInfoAction reads the recovered error from the context and
// returns it as a map. It is the default target for rules that want to
// inspect the original error without further processing.
var ErrorInfoAction = action.New("error.info", func(ctx context.Context, _ any) (map[string]any, error) {
	recovered, ok := contracts.RecoveredErrorFrom(ctx)
	if !ok || recovered == nil {
		return nil, xerr.NotFound("no recovered error in context")
	}
	return map[string]any{
		"kind":    recovered.Kind,
		"message": recovered.Err.Error(),
	}, nil
}).
	Description("Return information about the recovered error").
	Tag("error", "recovery").
	Build()
