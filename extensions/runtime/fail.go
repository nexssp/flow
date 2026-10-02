package runtime

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// Fail always returns an error. The @{ kind: "..." } arg selects the
// xerr kind; the default is Internal. It is the idiomatic way to
// simulate a specific failure for fallback pipelines.
var Fail = action.New("runtime.fail", func(_ context.Context, in any) (any, error) {
	m, _ := in.(map[string]any)
	msg := strings.TrimSpace(readStringArg(m, "message"))
	if msg == "" {
		msg = "fail: pipeline deliberately aborted by the fail action"
	}
	kind := strings.TrimSpace(readStringArg(m, "kind"))
	return nil, failError(kind, msg)
}).Description("Always return an error; kind configurable (default Internal)").
	Tag("base", "error").
	Build()

func failError(kind, msg string) *xerr.AppError {
	switch strings.ToLower(kind) {
	case "validation":
		return xerr.Validation(msg)
	case "badrequest", "bad_request":
		return xerr.BadRequest(msg)
	case "unauthorized":
		return xerr.Unauthorized(msg)
	case "forbidden":
		return xerr.Forbidden(msg)
	case "notfound", "not_found":
		return xerr.NotFound(msg)
	case "conflict":
		return xerr.Conflict(msg)
	case "timeout":
		return xerr.Timeout(msg)
	case "unavailable":
		return xerr.Unavailable(msg)
	case "toomanyrequests", "too_many_requests":
		return xerr.TooManyRequests(msg)
	default:
		return xerr.Internal(msg)
	}
}
