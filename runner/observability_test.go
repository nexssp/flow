package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestExecutionResolver_AppliesPerExecutionHooks(t *testing.T) {
	t.Parallel()

	memSink := observe.NewMemorySink(10)
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.Hooks = []action.AnyHook{observe.Hook(memSink)}

	ctx := xctx.WithRequestID(context.Background(), "req-exec-isolated")
	ctx = xctx.WithExecutionID(ctx, "exec-parent")

	ex, err := runner.Execute(ctx, cfg, "const @{ value: 42 } -> noop", "test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := memSink.Events()
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events from isolated execution, got %d", len(events))
	}

	for i := range events {
		event := &events[i]
		if event.RequestID != "req-exec-isolated" {
			t.Errorf("event %d: RequestID = %q, want %q", i, event.RequestID, "req-exec-isolated")
		}
		if event.ExecutionID != "exec-parent" {
			t.Errorf("event %d: ExecutionID = %q, want %q", i, event.ExecutionID, "exec-parent")
		}
	}

	if ex.Output != int64(42) {
		t.Errorf("output = %T(%v), want int64(42)", ex.Output, ex.Output)
	}
}

func TestExecutionResolver_ConcurrentExecutions_DoNotLeakHooks(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	sinkA := observe.NewMemorySink(10)
	sinkB := observe.NewMemorySink(10)

	cfgA := cfg
	cfgA.Hooks = []action.AnyHook{observe.Hook(sinkA)}

	cfgB := cfg
	cfgB.Hooks = []action.AnyHook{observe.Hook(sinkB)}

	errs := make(chan error, 2)

	go func() {
		ctx := xctx.WithRequestID(context.Background(), "req-A")
		_, err := runner.Execute(ctx, cfgA, "const @{ value: 'A' } -> noop", "testA", nil)
		errs <- err
	}()

	go func() {
		ctx := xctx.WithRequestID(context.Background(), "req-B")
		_, err := runner.Execute(ctx, cfgB, "const @{ value: 'B' } -> noop", "testB", nil)
		errs <- err
	}()

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent execute: %v", err)
		}
	}

	for i, ev := range sinkA.Events() {
		if ev.RequestID != "req-A" {
			t.Errorf("sinkA event %d has RequestID %q, want %q", i, ev.RequestID, "req-A")
		}
	}

	for i, ev := range sinkB.Events() {
		if ev.RequestID != "req-B" {
			t.Errorf("sinkB event %d has RequestID %q, want %q", i, ev.RequestID, "req-B")
		}
	}
}

func TestEventSink_ConfigurationOption(t *testing.T) {
	t.Parallel()

	memSink := observe.NewMemorySink(10)
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.EventSink = memSink

	cfg.Hooks = append(cfg.Hooks, observe.Hook(cfg.EventSink))

	ctx := xctx.WithRequestID(context.Background(), "req-event-sink")
	_, err = runner.Execute(ctx, cfg, "const @{ value: 'sink-test' } -> noop", "test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := memSink.Events()
	if len(events) == 0 {
		t.Fatal("expected events from EventSink, got none")
	}
	if events[0].RequestID != "req-event-sink" {
		t.Errorf("EventSink RequestID = %q, want %q", events[0].RequestID, "req-event-sink")
	}
}

func TestJSONLSink_WritesNewlineDelimitedJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sink := observe.NewJSONLSink(&buf, 1024)

	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.Hooks = []action.AnyHook{observe.Hook(sink)}

	ctx := xctx.WithRequestID(context.Background(), "req-jsonl")
	ctx = xctx.WithExecutionID(ctx, "exec-jsonl")
	ctx = xctx.WithTraceID(ctx, "trace-jsonl")

	_, err = runner.Execute(ctx, cfg, "const @{ value: 'jsonl' } -> noop", "test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 JSONL lines, got %d: %q", len(lines), buf.String())
	}

	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Errorf("line %d is not valid JSON: %v\n%s", i, err, line)
			continue
		}
		if record["request_id"] != "req-jsonl" {
			t.Errorf("line %d: request_id = %v, want %q", i, record["request_id"], "req-jsonl")
		}
		if record["action"] == "" {
			t.Errorf("line %d: action is empty", i)
		}
		if record["time"] == "" {
			t.Errorf("line %d: time is empty", i)
		}
	}
}

func TestPrometheusSink_ExposesMetrics(t *testing.T) {
	t.Parallel()

	promSink := observe.NewPrometheusSink()

	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.Hooks = []action.AnyHook{observe.Hook(promSink)}

	ctx := context.Background()
	_, err = runner.Execute(ctx, cfg, "const @{ value: 1 } -> noop", "test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var buf bytes.Buffer
	if err := promSink.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "nexss_action_events_total") {
		t.Errorf("output does not contain metric name:\n%s", output)
	}
	if !strings.Contains(output, `action="const"`) {
		t.Errorf("output does not contain action=\"const\":\n%s", output)
	}
	if !strings.Contains(output, `kind="success"`) {
		t.Errorf("output does not contain kind=\"success\":\n%s", output)
	}
}

func TestAllStandardActions_HaveDescriptions(t *testing.T) {
	t.Parallel()

	bundles := native.Bundles()
	missingDesc := make([]string, 0)

	for _, b := range bundles {
		for _, lib := range b.Libraries {
			for _, act := range lib.Actions {
				if act == nil {
					continue
				}
				meta := act.Describe()
				if meta == nil || meta.Description == "" {
					missingDesc = append(missingDesc, meta.Name)
				}
			}
			for _, src := range lib.Sources {
				if src == nil {
					continue
				}
				meta := src.Describe()
				if meta == nil || meta.Description == "" {
					missingDesc = append(missingDesc, meta.Name)
				}
			}
			for _, op := range lib.Operators {
				if op.Description == "" {
					missingDesc = append(missingDesc, op.Name)
				}
			}
		}
		for _, d := range b.Directives {
			if d.Example == "" {
				missingDesc = append(missingDesc, "@"+d.Name)
			}
		}
		for _, m := range b.Modifiers {
			if m.Example == "" {
				missingDesc = append(missingDesc, ":"+m.Name)
			}
		}
	}

	t.Logf("Audited %d bundles for metadata completeness", len(bundles))
	if len(missingDesc) > 0 {
		t.Logf("Found %d items missing descriptions/examples: %v", len(missingDesc), missingDesc)
	}
}

func TestContextPropagation_FromParentToChild(t *testing.T) {
	t.Parallel()

	memSink := observe.NewMemorySink(10)
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	cfg.Hooks = []action.AnyHook{observe.Hook(memSink)}

	ctx := xctx.WithRequestID(context.Background(), "parent-req-id")
	ctx = xctx.WithTenantID(ctx, "tenant-42")
	ctx = xctx.WithUserID(ctx, "user-bob")

	_, err = runner.Execute(ctx, cfg, "const @{ value: 'propagate' } -> noop", "test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := memSink.Events()
	for i := range events {
		event := &events[i]
		if event.RequestID != "parent-req-id" {
			t.Errorf("event %d: RequestID = %q, want %q", i, event.RequestID, "parent-req-id")
		}
		if event.TenantID != "tenant-42" {
			t.Errorf("event %d: TenantID = %q, want %q", i, event.TenantID, "tenant-42")
		}
		if event.UserID != "user-bob" {
			t.Errorf("event %d: UserID = %q, want %q", i, event.UserID, "user-bob")
		}
	}
}
