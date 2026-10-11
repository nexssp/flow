package on_error

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
)

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

// ErrorReraiseAction re-propagates the recovered error as a real error,
// so an enclosing `||` fallback (or the caller) can observe it.
//
// It works in both `? on_error` and `@on_error` contexts, reading the
// error from whichever source is available:
//
//   - Expression form: the recovery arm receives the error environment
//     map, whose `error.original` key carries the raw error value.
//   - Directive form: the wrapper stashes the recovered error in the
//     execution context via contracts.WithRecoveredError.
//
// If neither source yields an error, it returns xerr.Internal — being
// invoked with no error present is a programming error.
var ErrorReraiseAction = action.New("error.reraise", func(ctx context.Context, req any) (any, error) {
	if m, ok := req.(map[string]any); ok {
		if errMap, ok := m["error"].(map[string]any); ok {
			if original, ok := errMap["original"].(error); ok && original != nil {
				return nil, original
			}
		}
	}
	if recovered, ok := contracts.RecoveredErrorFrom(ctx); ok && recovered != nil {
		return nil, recovered.Err
	}
	return nil, xerr.Internal("error.reraise: no recovered error available")
}).
	Description("Propagate the recovered error back up the pipeline").
	Tag("error", "recovery").
	Build()

// ErrorPassthroughAction returns the original request, discarding the
// error envelope. It is the on_error counterpart of runtime.noop for
// the "I observed the error, now continue with the input unchanged"
// case.
//
// In the expression form, the action receives the error environment
// (a map carrying both `input` and `error`). We detect and unwrap the
// `input` key when the map also carries an `error` sub-map with a
// `kind` key — i.e. the shape recoveryEnvironment produces.
//
// The detection is deliberately narrow. A request that happens to
// have both an `error` map with a `kind` key and an `input` key will
// be unwrapped as well; this is a documented limitation and is
// unlikely in practice.
var ErrorPassthroughAction = action.New("error.passthrough", func(_ context.Context, req any) (any, error) {
	if m, ok := req.(map[string]any); ok {
		if errMap, ok := m["error"].(map[string]any); ok {
			if _, hasKind := errMap["kind"]; hasKind {
				if inner, hasInput := m["input"]; hasInput {
					return inner, nil
				}
			}
		}
	}
	return req, nil
}).
	Description("Return the original request, discarding the error envelope").
	Tag("error", "recovery").
	Build()
