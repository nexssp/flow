package progress

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler/contracts"
)

// StepAction renders one progress line and passes its input through
// unchanged, minus the message key.
//
//	{ x: 1 } -> progress.step @{ message: "Loading" } -> { y: 2 }
//
// After step runs, the pipeline sees { x: 1 }.
func StepAction() action.AnyAction {
	return action.New("progress.step", func(ctx context.Context, req any) (any, error) {
		message, cleaned, err := extractStepInput(req)
		if err != nil {
			return nil, err
		}
		if message != "" {
			xctx.ReportProgress(ctx, xctx.Progress{
				Message:  message,
				Current:  1,
				Total:    1,
				Metadata: map[string]any{"kind": "step"},
			})
		}
		return cleaned, nil
	}).
		Description("Emit a single progress line; input passes through").
		Tag("progress").
		Build()
}

func extractStepInput(req any) (message string, cleaned any, err error) {
	m, ok := req.(map[string]any)
	if !ok {
		return "", req, nil
	}
	message, err = readMessageOption(m, "progress.step")
	if err != nil {
		return "", nil, err
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "message" {
			continue
		}
		out[k] = v
	}
	if len(out) == 1 {
		if root, ok := out["__root__"]; ok {
			return message, root, nil
		}
	}
	return message, out, nil
}

func readMessageOption(values map[string]any, capability string) (string, error) {
	value, exists := values["message"]
	if !exists {
		return "", nil
	}
	message, ok := value.(string)
	if !ok {
		return "", xerr.Validation(capability + ": 'message' must be a string")
	}
	return message, nil
}

// WrapAction resolves its target through the execution resolver and
// runs the target inside a progress lifecycle. The target receives
// every field of the wrap input except the wrap-specific keys
// "action" and "message".
//
//	{ duration_ms: 2000 }
//	-> progress.wrap @{ action: runtime.sleep, message: "Slow op" }
func WrapAction() action.AnyAction {
	return action.New("progress.wrap", func(ctx context.Context, req any) (any, error) {
		resolver := contracts.ActionResolverFromContext(ctx)
		if resolver == nil {
			return nil, xerr.Internal("progress.wrap: no action resolver in execution context")
		}

		m, ok := req.(map[string]any)
		if !ok {
			return nil, xerr.BadRequest("progress.wrap: input must be an object")
		}

		targetName, _ := m["action"].(string)
		if targetName == "" {
			return nil, xerr.BadRequest("progress.wrap: 'action' is required")
		}
		message, messageErr := readMessageOption(m, "progress.wrap")
		if messageErr != nil {
			return nil, messageErr
		}
		if message == "" {
			message = targetName
		}

		target, found := resolver.Action(targetName)
		if !found {
			return nil, xerr.NotFound("progress.wrap: target " + targetName + " not registered")
		}

		payload := stripWrapKeys(m)

		xctx.ReportProgress(ctx, xctx.Progress{
			Message:  message,
			Current:  0,
			Total:    1,
			Metadata: map[string]any{"kind": "wrap"},
		})

		out, runErr := action.InvokeAny(ctx, target, payload)

		meta := map[string]any{"kind": "wrap"}
		if runErr != nil {
			meta["error"] = runErr.Error()
		}
		xctx.ReportProgress(ctx, xctx.Progress{
			Message:  message,
			Current:  1,
			Total:    1,
			Metadata: meta,
		})

		return out, runErr
	}).
		Description("Wrap a unary action with a progress lifecycle").
		Tag("progress").
		Build()
}

// stripWrapKeys removes the keys that progress.wrap consumes: "action",
// "message", and the injected __root__ sentinel. When the wrap was the
// only atom that saw a non-map pipeline input, the sentinel is
// unwrapped back into its original value so the target receives what
// the caller intended.
func stripWrapKeys(m map[string]any) any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch k {
		case "action", "message", "__root__":
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		if root, ok := m["__root__"]; ok {
			return root
		}
	}
	return out
}
