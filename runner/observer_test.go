package runner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

const observerTestSource = `runtime.const @{ value: { api_key: "topsecret", token: "response-secret", visible: "ok" } } -> probe.echo`

func TestObserverHookCapturesKernelLifecycleAndKeepsLegacyTrace(t *testing.T) {
	var live bytes.Buffer
	observer := runner.NewObserver(&live, 3)
	hook := observer.Hook()
	ctx := context.Background()
	meta := &action.Meta{Name: "test.observed"}
	req := map[string]any{"api_key": "topsecret", "visible": "request"}
	res := map[string]any{"token": "response-secret", "visible": "response"}

	hook.OnRetry(ctx, req, 2, errors.New("retry"), meta)
	hook.OnCacheHit(ctx, req, res, meta)
	hook.OnCacheMiss(ctx, req, meta)
	hook.OnTimeout(ctx, req, meta)
	hook.OnCancel(ctx, req, meta)
	hook.OnPanic(ctx, req, "panic-value", meta)
	hook.OnCoalesced(ctx, req, meta)
	hook.OnDeduplicated(ctx, req, meta)
	hook.OnSuccess(ctx, req, res, meta)
	hook.OnError(ctx, req, errors.New("action failed"), meta)

	wantKinds := []string{
		observe.KindRetry,
		observe.KindCacheHit,
		observe.KindCacheMiss,
		observe.KindTimeout,
		observe.KindCanceled,
		observe.KindPanic,
		observe.KindCoalesced,
		observe.KindDeduplicated,
		observe.KindSuccess,
		observe.KindError,
	}
	events := observer.Events()
	if len(events) != len(wantKinds) {
		t.Fatalf("captured %d events, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, kind := range wantKinds {
		if events[i].Kind != kind || events[i].Action != meta.Name {
			t.Errorf("event[%d] = {kind:%q action:%q}, want {kind:%q action:%q}", i, events[i].Kind, events[i].Action, kind, meta.Name)
		}
	}
	if events[0].Attempt != 2 || events[5].Recovered != nil {
		t.Fatalf("retry metadata or payload-free panic event is incorrect: retry=%+v panic=%+v", events[0], events[5])
	}

	if got := observer.ActionNames(); len(got) != 2 || got[0] != meta.Name || got[1] != meta.Name {
		t.Fatalf("legacy action names = %#v, want two terminal action rows", got)
	}
	if got := strings.Count(live.String(), "  ✓ test.observed") + strings.Count(live.String(), "  ✗ test.observed"); got != 2 {
		t.Fatalf("live trace printed %d terminal rows, want 2; output:\n%s", got, live.String())
	}
	if strings.Contains(live.String(), "topsecret") || strings.Contains(live.String(), "response-secret") || !strings.Contains(live.String(), "[REDACTED]") {
		t.Fatalf("verbose trace did not preserve Flow redaction:\n%s", live.String())
	}
	if strings.Contains(live.String(), "retry") || strings.Contains(live.String(), "cache_hit") {
		t.Fatalf("non-terminal Kernel events changed legacy CLI live output:\n%s", live.String())
	}
	var summary bytes.Buffer
	observer.PrintSummary(&summary)
	if got := strings.Count(summary.String(), "test.observed"); got != 2 {
		t.Fatalf("legacy summary contains %d action rows, want 2:\n%s", got, summary.String())
	}
}

func TestObserverSnapshotDoesNotExposePayloadSecrets(t *testing.T) {
	observer := runner.NewObserver(nil, 0)
	hook := observer.Hook()
	hook.OnSuccess(context.Background(),
		map[string]any{"api_key": "request-secret", "visible": "request"},
		map[string]any{"password": "response-secret", "visible": "response"},
		&action.Meta{Name: "test.private"},
	)

	events := observer.Events()
	if len(events) != 1 {
		t.Fatalf("snapshot has %d events, want one", len(events))
	}
	event := events[0]
	if event.Request != nil || event.Response != nil || event.Error != nil || event.Recovered != nil {
		t.Fatalf("Flow-owned snapshot retained payload data: %+v", event)
	}
	if snapshot := fmt.Sprintf("%#v", events); strings.Contains(snapshot, "request-secret") || strings.Contains(snapshot, "response-secret") {
		t.Fatalf("secret was retrievable from Flow-owned snapshot: %s", snapshot)
	}
	if event.Kind != observe.KindSuccess || event.Action != "test.private" {
		t.Fatalf("payload stripping lost lifecycle metadata: %+v", event)
	}
}

func TestObserverEventSnapshotIsBoundedAndConcurrencySafe(t *testing.T) {
	observer := runner.NewObserver(nil, 0)
	const callers = 8
	const each = 128
	var wg sync.WaitGroup
	for caller := range callers {
		wg.Add(1)
		go func(caller int) {
			defer wg.Done()
			for i := range each {
				observer.Emit(context.Background(), observe.Event{
					Kind:   observe.KindRetry,
					Action: fmt.Sprintf("worker-%d-event-%d", caller, i),
				})
			}
		}(caller)
	}
	wg.Wait()

	events := observer.Events()
	if len(events) != 256 {
		t.Fatalf("bounded snapshot has %d events, want capacity 256", len(events))
	}
	for _, event := range events {
		if event.Kind != observe.KindRetry || !strings.HasPrefix(event.Action, "worker-") {
			t.Fatalf("corrupt or unexpected concurrent event: %+v", event)
		}
	}
	events[0].Action = "mutated snapshot"
	if got := observer.Events()[0].Action; got == "mutated snapshot" {
		t.Fatal("modifying an event snapshot changed the observer's retained events")
	}
}

func TestExecute_ConfigEventSinkReceivesKernelLifecycleEvents(t *testing.T) {
	cfg := observerTestConfig(t)
	sink := &eventCollector{}
	cfg.EventSink = sink

	_, err := runner.Execute(t.Context(), cfg, observerTestSource, "observer-event-sink.nflow", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, event := range sink.Events() {
		if event.Action == "probe.echo" && event.Kind == observe.KindSuccess {
			response, ok := event.Response.(map[string]any)
			if !ok || response["api_key"] != "topsecret" || response["token"] != "response-secret" {
				t.Fatalf("explicit Config.EventSink did not receive the unmodified Kernel payload: %+v", event)
			}
			return
		}
	}
	t.Fatalf("Config.EventSink did not receive probe.echo lifecycle event: %+v", sink.Events())
}

func TestRunSourcePreservesLegacyVerbosityAndTraceRedaction(t *testing.T) {
	cfg := observerTestConfig(t)
	for _, tc := range []struct {
		verbosity int
		want      []string
		absent    []string
	}{
		{verbosity: 0, want: []string{`{"api_key":"topsecret","token":"response-secret","visible":"ok"}`}, absent: []string{"EXECUTION TRACE", "      in:  ", "✓ probe.echo"}},
		{verbosity: 1, want: []string{"✓ map[api_key:topsecret token:response-secret visible:ok]", "compile:", "run:", "EXECUTION TRACE"}, absent: []string{"      in:  "}},
		{verbosity: 2, want: []string{"✓ probe.echo", "EXECUTION TRACE"}, absent: []string{"      in:  "}},
		{verbosity: 3, want: []string{"✓ probe.echo", "EXECUTION TRACE", `"api_key":"[REDACTED]"`, `"token":"[REDACTED]"`}},
	} {
		var stdout, stderr bytes.Buffer
		code := runner.RunSource(t.Context(), cfg, observerTestSource, "observer-output.nflow", nil, runner.Opts{
			Verbosity: tc.verbosity,
			Stdout:    &stdout,
			Stderr:    &stderr,
		})
		if code != 0 {
			t.Fatalf("verbosity %d: RunSource returned %d, stderr=%s", tc.verbosity, code, stderr.String())
		}
		got := stdout.String()
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("verbosity %d output missing %q:\n%s", tc.verbosity, want, got)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(got, absent) {
				t.Errorf("verbosity %d output unexpectedly contains %q:\n%s", tc.verbosity, absent, got)
			}
		}
		if tc.verbosity == 0 && got != `{"api_key":"topsecret","token":"response-secret","visible":"ok"}`+"\n" {
			t.Errorf("verbosity 0 stdout changed: got %q", got)
		}
		if tc.verbosity >= 3 {
			for line := range strings.SplitSeq(got, "\n") {
				if (strings.Contains(line, "      in:  ") || strings.Contains(line, "      out: ")) &&
					(strings.Contains(line, "topsecret") || strings.Contains(line, "response-secret")) {
					t.Errorf("verbosity 3 trace payload was not redacted: %s", line)
				}
			}
		}
	}
}

func observerTestConfig(t *testing.T) runner.Config {
	t.Helper()
	echo := action.New("probe.echo", func(_ context.Context, input any) (any, error) {
		return input, nil
	}).Build()
	bundles := append(native.Bundles(), compiler.Bundle{
		ID: "observer-regression-test",
		Libraries: []action.Library{{
			Name:    "probe",
			Actions: []action.AnyAction{echo},
		}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	return cfg
}

type eventCollector struct {
	mu     sync.Mutex
	events []observe.Event
}

func (c *eventCollector) Emit(_ context.Context, event observe.Event) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

func (c *eventCollector) Events() []observe.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]observe.Event(nil), c.events...)
}
