package nodes_supervisor

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest"

	"github.com/nexssp/flow/compiler/contracts"
)

type actionMap map[string]action.AnyAction

func (m actionMap) Action(name string) (action.AnyAction, bool) {
	act, ok := m[name]
	return act, ok
}

func runServe(ctx context.Context, resolver actionMap, req ServeReq) error {
	ctx = contracts.WithActionResolver(ctx, resolver)
	_, err := action.InvokeAny(ctx, Serve, req)
	return err
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestServeStartsDependentsOnlyAfterReadinessAndCancelsCleanly(t *testing.T) {
	baseline := testingGoroutineCount()
	parentReady := make(chan struct{})
	dependentStarted := make(chan struct{})

	parent := action.New("test.parent", func(ctx context.Context, _ any) (any, error) {
		// Publish the marker before SignalReady releases dependent goroutines.
		close(parentReady)
		contracts.SignalReady(ctx)
		<-ctx.Done()
		return nil, ctx.Err()
	}).Build()
	dependent := action.New("test.dependent", func(ctx context.Context, _ any) (any, error) {
		select {
		case <-parentReady:
		default:
			return nil, errors.New("dependent started before its dependency was ready")
		}
		close(dependentStarted)
		contracts.SignalReady(ctx)
		<-ctx.Done()
		return nil, ctx.Err()
	}).Build()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServe(ctx, actionMap{"test.parent": parent, "test.dependent": dependent}, ServeReq{
			Services: []ServiceSpec{
				{ID: "api", Action: "test.parent"},
				{ID: "worker", Action: "test.dependent", DependsOn: []string{"api"}},
			},
		})
	}()
	waitSignal(t, dependentStarted, "dependent service startup")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("parent cancellation should be clean, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not join services after cancellation")
	}
	xtest.RequireNoGoroutineLeak(t, baseline, time.Second)
}

func TestServeTreatsSpontaneousContextCanceledAsUnexpectedExit(t *testing.T) {
	dependentCalls := atomic.Int32{}
	upstream := action.New("test.upstream", func(context.Context, any) (any, error) {
		return nil, context.Canceled
	}).Build()
	dependent := action.New("test.downstream", func(context.Context, any) (any, error) {
		dependentCalls.Add(1)
		return nil, nil
	}).Build()

	err := runServe(context.Background(), actionMap{
		"test.upstream":   upstream,
		"test.downstream": dependent,
	}, ServeReq{Services: []ServiceSpec{
		{ID: "upstream", Action: "test.upstream"},
		{ID: "downstream", Action: "test.downstream", DependsOn: []string{"upstream"}},
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("spontaneous context.Canceled error = %v, want wrapped context.Canceled", err)
	}
	if dependentCalls.Load() != 0 {
		t.Fatalf("dependent ran %d times after its service failed before readiness", dependentCalls.Load())
	}
}

func TestServeCancelsSiblingAndPreservesServiceFailure(t *testing.T) {
	siblingStarted := make(chan struct{})
	siblingStopped := make(chan struct{})
	wantErr := errors.New("listener failed")

	sibling := action.New("test.sibling", func(ctx context.Context, _ any) (any, error) {
		close(siblingStarted)
		<-ctx.Done()
		close(siblingStopped)
		return nil, ctx.Err()
	}).Build()
	failing := action.New("test.failing", func(context.Context, any) (any, error) {
		<-siblingStarted
		return nil, wantErr
	}).Build()

	err := runServe(context.Background(), actionMap{
		"test.sibling": sibling,
		"test.failing": failing,
	}, ServeReq{Services: []ServiceSpec{
		{ID: "listener", Action: "test.failing"},
		{ID: "other", Action: "test.sibling"},
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("supervisor error = %v, want original service error", err)
	}
	select {
	case <-siblingStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("failing service did not cancel its sibling")
	}
}

func TestServeRejectsNilReturnAndRecoversPanics(t *testing.T) {
	for _, test := range []struct {
		name string
		act  action.AnyAction
		want string
	}{
		{
			name: "nil return",
			act: action.New("test.service", func(context.Context, any) (any, error) {
				return nil, nil
			}).Build(),
			want: "exited unexpectedly without an error",
		},
		{
			name: "panic",
			act: action.New("test.service", func(context.Context, any) (any, error) {
				panic("test panic")
			}).Build(),
			want: "panic recovered: test panic",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runServe(context.Background(), actionMap{"test.service": test.act}, ServeReq{
				Services: []ServiceSpec{{ID: "service", Action: "test.service"}},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("supervisor error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestServeValidatesRuntimeGraphsBeforeStartingServices(t *testing.T) {
	service := action.New("test.service", func(context.Context, any) (any, error) {
		return nil, nil
	}).Build()
	for _, test := range []struct {
		name     string
		services []ServiceSpec
		want     string
	}{
		{
			name: "duplicate ids",
			services: []ServiceSpec{
				{ID: "same", Action: "test.service"},
				{ID: "same", Action: "test.service"},
			},
			want: "duplicate service id",
		},
		{
			name:     "unknown dependency",
			services: []ServiceSpec{{ID: "service", Action: "test.service", DependsOn: []string{"missing"}}},
			want:     "unknown service",
		},
		{
			name: "cycle",
			services: []ServiceSpec{
				{ID: "a", Action: "test.service", DependsOn: []string{"b"}},
				{ID: "b", Action: "test.service", DependsOn: []string{"a"}},
			},
			want: "dependency cycle",
		},
		{
			name: "enabled service depends on disabled service",
			services: []ServiceSpec{
				{ID: "off", Action: "test.service", Enabled: new(false)},
				{ID: "on", Action: "test.service", DependsOn: []string{"off"}},
			},
			want: "depends on disabled service",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runServe(context.Background(), actionMap{"test.service": service}, ServeReq{Services: test.services})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("supervisor error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestServeRejectsAllDisabledServices(t *testing.T) {
	service := action.New("test.service", func(context.Context, any) (any, error) {
		return nil, nil
	}).Build()
	err := runServe(context.Background(), actionMap{"test.service": service}, ServeReq{
		Services: []ServiceSpec{{ID: "service", Action: "test.service", Enabled: new(false)}},
	})
	if err == nil || !strings.Contains(err.Error(), "no enabled services") {
		t.Fatalf("all-disabled graph error = %v, want no-enabled-services validation", err)
	}
}

func TestServeResolvesActionsBeforeStartingAnyService(t *testing.T) {
	calls := atomic.Int32{}
	service := action.New("test.available", func(context.Context, any) (any, error) {
		calls.Add(1)
		return nil, nil
	}).Build()
	err := runServe(context.Background(), actionMap{"test.available": service}, ServeReq{
		Services: []ServiceSpec{
			{ID: "available", Action: "test.available"},
			{ID: "missing", Action: "test.missing"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "test.missing not found") {
		t.Fatalf("missing action error = %v, want action lookup failure", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("started %d services before resolving the complete action set", calls.Load())
	}
}

func testingGoroutineCount() int {
	return runtime.NumGoroutine()
}
