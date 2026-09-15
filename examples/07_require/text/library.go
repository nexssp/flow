// Package text provides a minimal flow.Library used by the m6_require
// example. It demonstrates:
//
//  1. The one convention every library must follow: export
//     func Library() flow.Library.
//
//  2. The typing rule the codebase follows: request and response
//     payloads are structs, not map[string]any.
//
//  3. The testing convention: expose each typed action builder as its
//     own function, so tests can call testkit.BenchAction,
//     testkit.Simulate, and act.Do directly without unwrapping.
package text

import (
	"context"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

// UppercaseReq is the input contract of text_tools.uppercase.
type UppercaseReq struct {
	Message string `json:"message" validate:"required"`
}

// UppercaseRes is the output contract.
type UppercaseRes struct {
	Message string `json:"message"`
}

// Uppercase returns the typed action builder. Tests use it directly;
// the library wraps it as an AnyAction for the runtime.
func Uppercase() *action.BuiltAction[UppercaseReq, UppercaseRes] {
	return action.New("text_tools.uppercase", uppercase).
		Description("Uppercases the 'message' field of the input").
		Tag("text", "transform").
		Build()
}

// Library is the entire public surface the flow runtime sees.
func Library() flow.Library {
	return flow.Library{
		Name:        "text",
		Description: "Text manipulation utilities for flow examples",
		Actions:     []action.AnyAction{Uppercase()},
		Aliases: []flow.Alias{
			{Canonical: "text_tools.uppercase", Short: []string{"uppercase", "upper"}},
		},
	}
}

func uppercase(_ context.Context, req UppercaseReq) (UppercaseRes, error) {
	return UppercaseRes{Message: strings.ToUpper(req.Message)}, nil
}
