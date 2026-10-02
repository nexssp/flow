package projection

import (
	"context"
	"maps"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
)

// projectionAction builds the runtime "projection" action bound to
// eval. The action receives the raw projection body under "raw" plus
// the current pipeline state as the remaining fields.
func projectionAction(eval Evaluator) action.AnyAction {
	return action.New("projection.project",
		func(ctx context.Context, input map[string]any) (any, error) {
			raw, _ := input["raw"].(string)

			state := make(map[string]any, len(input))
			for key, value := range input {
				if key != "raw" {
					state[key] = value
				}
			}

			// If the compiler injected __root__ as the only key, project
			// over that root instead of the raw input map.
			if root, ok := state["__root__"]; ok && len(state) == 1 {
				return eval(ctx, Request{Raw: raw, Input: root})
			}
			return eval(ctx, Request{Raw: raw, Input: state})
		}).
		Description("Evaluate a projection body").
		Tag("compiler", "projection").
		Build()
}

// spreadMergeOption registers the __spread__ builtin used by the
// rewritten expression for `{ ...base, x: 1 }` forms.
var spreadMergeOption = expr.Function(
	"__spread__",
	func(params ...any) (any, error) {
		out := make(map[string]any, len(params)*4)
		for _, p := range params {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			maps.Copy(out, m)
		}
		return out, nil
	},
)
