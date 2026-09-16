package text

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type UppercaseReq struct {
	Message string `json:"message" validate:"required"`
}

type UppercaseRes struct {
	Message string `json:"message"`
}

func Uppercase() *action.BuiltAction[UppercaseReq, UppercaseRes] {
	return action.New("text_tools.uppercase", uppercase).
		Description("Uppercases the 'message' field of the input").
		Tag("text", "transform").
		Route(thttp.POST("/api/text/uppercase")).
		Build()
}

func Library() action.Library {
	return action.Library{
		Name:        "text",
		Description: "Text manipulation utilities for flow examples",
		Actions:     []action.AnyAction{Uppercase()},
		Aliases: []action.Alias{
			{Canonical: "text_tools.uppercase", Short: []string{"uppercase", "upper"}},
		},
	}
}

func uppercase(_ context.Context, req UppercaseReq) (UppercaseRes, error) {
	return UppercaseRes{Message: strings.ToUpper(req.Message)}, nil
}
