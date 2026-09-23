package nodes

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// PanicReq is the request DTO for the `panic` action.
type PanicReq struct {
	Msg string `json:"msg" cli:"msg" usage:"Message passed to panic()"`
}

// NewPanicAction returns an action that panics. Use it in tests, guardrails,
// and escalation chains (`:onerror=["panic :msg=critical"]`).
//
// A panic from this action is caught by BuiltAction.Do's recover() and
// converted to an error. In the DSL it is testable via `@onpanic`.
//
// Distinct from `fail`:
//   - fail  → returns an error (recoverable, expected)
//   - panic → unreachable state, invariant violation, bug
func NewPanicAction() action.AnyAction {
	return action.New("panic", func(_ context.Context, req PanicReq) (struct{}, error) {
		msg := req.Msg
		if msg == "" {
			msg = "explicit panic"
		}
		panic(xerr.Internal(msg).Error())
	}).Description("Raise a panic (caught by action recover)").
		Tag("utility", "testing").
		Build()
}
