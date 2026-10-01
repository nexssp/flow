package decide

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// DecideAction evaluates a state against a registered backend.
var DecideAction = action.New("decide", func(ctx context.Context, req Request) (Response, error) {
	if req.Backend == "" {
		return Response{}, xerr.BadRequest("decide: backend is required")
	}
	if len(req.Questions) == 0 {
		return Response{}, xerr.BadRequest("decide: questions map is required")
	}

	backend, exists := Lookup(req.Backend)
	if !exists {
		return Response{}, xerr.NotFound("decide: backend " + req.Backend + " not registered")
	}

	wireQuestions := make(map[string]any, len(req.Questions))
	for key, question := range req.Questions {
		wireQuestions[key] = question
	}

	result, err := backend.Decide(ctx, req.State, wireQuestions)
	if err != nil {
		return Response{}, xerr.Unavailable("decide: backend "+req.Backend+" execution failed", err)
	}
	return result, nil
}).
	Description("Evaluates state with a decision backend").
	Tag("ai", "decision").
	Build()
