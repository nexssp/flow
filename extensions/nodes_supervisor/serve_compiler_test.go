package nodes_supervisor_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler/contracts"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type actionMap map[string]action.AnyAction

func (m actionMap) Action(name string) (action.AnyAction, bool) {
	act, ok := m[name]
	return act, ok
}

// Compile-invalid graph cases stay in Go tests because the fixture runner
// does not support expected compile errors.
func TestServeAtomAdviceValidatesLiteralGraphsWithFlowCompiler(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "duplicate service id",
			src: `supervisor.serve @{ services: [
				{id: "api", action: "thttp.listen"},
				{id: "api", action: "tworker.listen"}
			] }`,
			want: `duplicate service id "api"`,
		},
		{
			name: "unknown dependency",
			src: `supervisor.serve @{ services: [
				{id: "worker", action: "tworker.listen", depends_on: ["missing"]}
			] }`,
			want: `depends on unknown service "missing"`,
		},
		{
			name: "dependency cycle",
			src: `supervisor.serve @{ services: [
				{id: "api", action: "thttp.listen", depends_on: ["worker"]},
				{id: "worker", action: "tworker.listen", depends_on: ["api"]}
			] }`,
			want: "dependency cycle",
		},
		{
			name: "dynamic service graph rejected",
			src:  `supervisor.serve @{ services: @.services }`,
			want: "non-empty literal list",
		},
		{
			name: "typo rejected",
			src: `supervisor.serve @{ services: [
				{id: "api", action: "thttp.listen", depend_on: ["db"]}
			] }`,
			want: `unknown field "depend_on"`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := runner.Compile(t.Context(), cfg, test.src, "serve-compile-test.nflow")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("compile error = %v, want %q", err, test.want)
			}
		})
	}
}

// Keep the runtime-reference sample inline so its compile and run cases share
// the exact source while the documentation keeps its own user-facing example.
const serveRuntimeRefsExample = `@pipeline backend_services
  supervisor.serve @{
    services: [
      { id: "http", action: "thttp.listen", payload: .http_config },
      { id: "worker", action: "tworker.listen", depends_on: ["http"], enabled: .worker_enabled, payload: .worker_config }
    ]
  }
@end`

// Fixtures cannot supply the mock action resolver and typed pipeline input
// needed to exercise runtime field-reference resolution.
func TestServeRuntimeRefsExampleCompiles(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Compile(t.Context(), cfg, serveRuntimeRefsExample, "serve-runtime-refs.nflow"); err != nil {
		t.Fatalf("compile serve runtime-refs example: %v", err)
	}
}

func TestServeRuntimeRefsExampleRunsWithPipelineInput(t *testing.T) {
	source := serveRuntimeRefsExample + "\npipeline.backend_services\n"
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runner.Compile(t.Context(), cfg, source, "serve-runtime-refs.nflow")
	if err != nil {
		t.Fatalf("compile serve runtime-refs example: %v", err)
	}

	httpConfig := map[string]any{"addr": "127.0.0.1:8787", "read_timeout": "3s"}
	workerConfig := map[string]any{"queue": "integration", "workers": 3}
	for _, test := range []struct {
		name          string
		workerEnabled bool
		wantWorker    bool
	}{
		{name: "worker disabled", workerEnabled: false, wantWorker: false},
		{name: "worker enabled", workerEnabled: true, wantWorker: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			type serviceCall struct {
				name    string
				payload any
			}
			calls := make(chan serviceCall, 2)
			serviceAction := func(name string) action.AnyAction {
				return action.New(name, func(ctx context.Context, payload any) (any, error) {
					calls <- serviceCall{name: name, payload: payload}
					contracts.SignalReady(ctx)
					<-ctx.Done()
					return nil, ctx.Err()
				}).Build()
			}
			resolver := actionMap{
				"thttp.listen":   serviceAction("thttp.listen"),
				"tworker.listen": serviceAction("tworker.listen"),
			}
			ctx, cancel := context.WithCancel(contracts.WithActionResolver(t.Context(), resolver))
			defer cancel()
			input := map[string]any{
				"http_config":    httpConfig,
				"worker_enabled": test.workerEnabled,
				"worker_config":  workerConfig,
			}
			done := make(chan error, 1)
			go func() {
				_, err := action.InvokeAny(ctx, compiled.Program, input)
				done <- err
			}()

			wantCalls := 1
			if test.wantWorker {
				wantCalls++
			}
			seen := make(map[string]any, wantCalls)
			for range wantCalls {
				select {
				case call := <-calls:
					if _, duplicate := seen[call.name]; duplicate {
						t.Fatalf("service %q was invoked more than once", call.name)
					}
					seen[call.name] = call.payload
				case err := <-done:
					t.Fatalf("runtime-refs pipeline completed before services started: %v", err)
				case <-time.After(3 * time.Second):
					t.Fatal("runtime-ref services did not start with the supplied pipeline input")
				}
			}
			if got := seen["thttp.listen"]; !reflect.DeepEqual(got, httpConfig) {
				t.Errorf("HTTP payload = %#v, want %#v", got, httpConfig)
			}
			if test.wantWorker {
				if got := seen["tworker.listen"]; !reflect.DeepEqual(got, workerConfig) {
					t.Errorf("worker payload = %#v, want %#v", got, workerConfig)
				}
			} else {
				select {
				case call := <-calls:
					t.Errorf("disabled service %q was invoked with payload %#v", call.name, call.payload)
				case <-time.After(50 * time.Millisecond):
				}
			}

			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("runtime-refs pipeline did not stop cleanly after cancellation: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("runtime-refs pipeline did not join its services after cancellation")
			}
		})
	}
}

// Bare action names require a test resolver and cannot run in a self-test fixture.
func TestBareActionNameResolvesThroughActualFlowCompiler(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runner.Compile(t.Context(), cfg, `supervisor.serve @{ services: [
		{id: "service", action: test.ready}
	] }`, "serve-bare-action.nflow")
	if err != nil {
		t.Fatalf("compile bare action name with Flow v0.49.0: %v", err)
	}

	started := make(chan struct{})
	service := action.New("test.ready", func(ctx context.Context, _ any) (any, error) {
		contracts.SignalReady(ctx)
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}).Build()
	resolver := actionMap{"test.ready": service}
	ctx, cancel := context.WithCancel(contracts.WithActionResolver(context.Background(), resolver))
	done := make(chan error, 1)
	go func() {
		_, err := action.InvokeAny(ctx, compiled.Program, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("bare action value did not resolve to the service action")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("service shutdown after parent cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("compiled supervisor did not stop after cancellation")
	}
}
