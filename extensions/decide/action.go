package decide

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// DecideAction evaluates a state against a registered backend.
var DecideAction = action.New("decide.run", func(ctx context.Context, req Request) (Response, error) {
	if req.Backend == "" {
		return Response{}, xerr.BadRequest("decide: backend is required")
	}
	if len(req.Questions) == 0 {
		return Response{}, xerr.BadRequest("decide: questions map is required")
	}
	if req.TimeoutMS < 0 || req.TimeoutMS > int64(math.MaxInt64)/int64(time.Millisecond) {
		return Response{}, xerr.BadRequest("decide: timeout_ms is outside the supported range")
	}

	backend, exists := Lookup(req.Backend)
	if !exists {
		return Response{}, xerr.NotFound("decide: backend " + req.Backend + " not registered")
	}

	wireQuestions := make(map[string]any, len(req.Questions))
	for key, question := range req.Questions {
		wireQuestions[key] = question
	}

	callCtx := ctx
	if req.TimeoutMS > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	result, err := backend.Decide(callCtx, req.State, wireQuestions)
	if err != nil {
		message := "decide: backend " + req.Backend + " execution failed"
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return Response{}, xerr.Timeout(message, err)
		}
		if errors.Is(callCtx.Err(), context.Canceled) {
			return Response{}, xerr.Canceled(message, err)
		}
		if appErr, ok := errors.AsType[*xerr.AppError](err); ok && appErr != nil {
			return Response{}, fmt.Errorf("%s: %w", message, err)
		}
		return Response{}, xerr.Unavailable(message, err)
	}
	return result, nil
}).
	Description("Evaluates state with a decision backend").
	Tag("ai", "decision").
	Build()
