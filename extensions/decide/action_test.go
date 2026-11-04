package decide

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/xerr"
)

type actionTestBackend struct {
	name   string
	decide func(context.Context, map[string]any, map[string]any) (Result, error)
}

func (b actionTestBackend) Name() string { return b.name }

func (b actionTestBackend) Decide(ctx context.Context, state, questions map[string]any) (Result, error) {
	return b.decide(ctx, state, questions)
}

func (actionTestBackend) Close() error { return nil }

func TestDecideActionUsesTimeoutMS(t *testing.T) {
	observedDeadline := make(chan time.Duration, 1)
	backendName := "test.timeout." + t.Name()
	if err := Register(actionTestBackend{
		name: backendName,
		decide: func(ctx context.Context, _, _ map[string]any) (Result, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				return Result{}, errors.New("backend context has no deadline")
			}
			observedDeadline <- time.Until(deadline)
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}

	parentCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := DecideAction.Do(parentCtx, Request{
		Backend:   backendName,
		Questions: map[string]Question{"decision": {Type: "label"}},
		TimeoutMS: 100,
	})
	if got := xerr.KindFrom(err); got != xerr.KindTimeout {
		t.Fatalf("error kind = %s, want %s: %v", got, xerr.KindTimeout, err)
	}
	if remaining := <-observedDeadline; remaining <= 0 || remaining > 100*time.Millisecond {
		t.Fatalf("backend deadline remaining = %s, want (0, 100ms]", remaining)
	}
}

func TestDecideActionPreservesBackendErrorKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want xerr.Kind
	}{
		{name: "bad request", err: xerr.BadRequest("invalid input"), want: xerr.KindBadRequest},
		{name: "wrapped bad request", err: fmt.Errorf("provider: %w", xerr.BadRequest("invalid input")), want: xerr.KindBadRequest},
		{name: "timeout", err: xerr.Timeout("provider timed out"), want: xerr.KindTimeout},
		{name: "untyped", err: errors.New("connection failed"), want: xerr.KindUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			backendName := "test.error." + t.Name()
			if err := Register(actionTestBackend{
				name: backendName,
				decide: func(context.Context, map[string]any, map[string]any) (Result, error) {
					return Result{}, test.err
				},
			}); err != nil {
				t.Fatal(err)
			}
			_, err := DecideAction.Do(t.Context(), Request{
				Backend:   backendName,
				Questions: map[string]Question{"decision": {Type: "label"}},
			})
			if got := xerr.KindFrom(err); got != test.want {
				t.Fatalf("error kind = %s, want %s: %v", got, test.want, err)
			}
			if !strings.Contains(err.Error(), backendName) {
				t.Fatalf("error = %v, want backend context for %q", err, backendName)
			}
		})
	}
}

func TestDecideActionRejectsInvalidTimeoutMS(t *testing.T) {
	maxTimeoutMS := int64(math.MaxInt64) / int64(time.Millisecond)
	for _, timeout := range []int64{-1, maxTimeoutMS + 1} {
		_, err := DecideAction.Do(t.Context(), Request{
			Backend:   "unregistered",
			Questions: map[string]Question{"decision": {Type: "label"}},
			TimeoutMS: timeout,
		})
		if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
			t.Fatalf("timeout_ms %d error kind = %s, want %s: %v", timeout, got, xerr.KindBadRequest, err)
		}
	}
}
