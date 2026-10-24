package on_error

import (
	"context"
	"errors"

	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/match"
)

func terminalRecoveryError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func recoveryEnvironment(ctx context.Context, input any, err error) map[string]any {
	env := compiler.BuildEnv(input)
	env["input"] = input
	env["error"] = errorMetadata(err)
	return match.WithSymbolEnvironment(ctx, env)
}

func pipelineErrorEnvironment(ctx context.Context, input any, err error) map[string]any {
	metadata := errorMetadata(err)
	metadata["kind"] = string(xerr.From(err).Kind)
	env := map[string]any{"input": input, "error": metadata}
	return match.WithSymbolEnvironment(ctx, env)
}

func errorMetadata(err error) map[string]any {
	kind := string(xerr.From(err).Kind)
	message := err.Error()
	var cause error
	var details xerr.ValidationDetails
	if appErr, ok := errors.AsType[*xerr.AppError](err); ok {
		if appErr.Message != "" {
			message = appErr.Message
		}
		cause = appErr.Cause
		details = appErr.ValidationDetails
	}
	if cause == nil {
		cause = errors.Unwrap(err)
	}
	return map[string]any{
		"kind":     kind,
		"message":  message,
		"original": err,
		"cause":    cause,
		"details":  details,
	}
}
