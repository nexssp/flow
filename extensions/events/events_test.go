package events_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/events"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type readyPayload struct {
	BindAddress string `json:"bind_address"`
}

type emitRequest struct {
	BindAddress string `json:"bind_address"`
}

type unsupportedEventPayload struct {
	Values map[int]string `json:"values"`
}

type recordingPublisher struct {
	mu              sync.Mutex
	subjects        []string
	payloads        []any
	routeInvocation bool
}

func (publisher *recordingPublisher) PublishEvent(ctx context.Context, subject string, payload any) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.subjects = append(publisher.subjects, subject)
	publisher.payloads = append(publisher.payloads, payload)
	publisher.routeInvocation = events.IsRouteInvocation(ctx)
	return nil
}

type memoryJournal struct {
	mu      sync.Mutex
	steps   []string
	results map[string][]byte
}

func (journal *memoryJournal) GetStep(_ context.Context, executionID, stepName string) (result []byte, found bool, err error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	value, ok := journal.results[executionID+"\x00"+stepName]
	return append([]byte(nil), value...), ok, nil
}

func (journal *memoryJournal) RecordStep(_ context.Context, executionID, stepName string, payload []byte) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.results == nil {
		journal.results = make(map[string][]byte)
	}
	key := executionID + "\x00" + stepName
	journal.results[key] = append([]byte(nil), payload...)
	journal.steps = append(journal.steps, stepName)
	return nil
}

func (journal *memoryJournal) recordedSteps() []string {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return append([]string(nil), journal.steps...)
}

func TestDirectiveParsesDirectSinkSymbolAndPositions(t *testing.T) {
	source := "@events {\n" +
		"  observe sample.events.ready -> sample.sink\n" +
		"}\n"
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(events.Directive), source, "routes.nflow")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	routes := events.RoutesFromMeta(meta)
	if len(routes) != 1 {
		t.Fatalf("routes = %#v, want one route", routes)
	}
	if routes[0].EventName != "sample.events.ready" || routes[0].SinkName != "sample.sink" {
		t.Fatalf("route = %#v", routes[0])
	}
	if routes[0].Position != (compiler.Position{File: "routes.nflow", Line: 2, Col: 3}) {
		t.Fatalf("route position = %#v", routes[0].Position)
	}
	if routes[0].SinkPosition.Line != 2 {
		t.Fatalf("sink position = %#v", routes[0].SinkPosition)
	}
}

func TestDirectiveParsesInlineCommentsOnRouteLines(t *testing.T) {
	for _, tc := range []struct {
		name    string
		comment string
	}{
		{name: "hash", comment: "# inline note"},
		{name: "slash", comment: "// inline note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "@events {\n  observe sample.events.ready -> sample.sink " + tc.comment + "\n}\n"
			_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(events.Directive), source, "commented-route.nflow")
			if err != nil {
				t.Fatalf("preprocess route with inline comment: %v", err)
			}
			routes := events.RoutesFromMeta(meta)
			if len(routes) != 1 || routes[0].SinkName != "sample.sink" {
				t.Fatalf("routes = %#v, want one route to sample.sink", routes)
			}
		})
	}
}

func TestDirectiveParsesColumnZeroHashCommentOnLaterLine(t *testing.T) {
	source := "@events {\n" +
		"# don't route this\n" +
		"  observe sample.events.ready -> sample.sink\n" +
		"}\n"
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(events.Directive), source, "column-zero-comment.nflow")
	if err != nil {
		t.Fatalf("preprocess route after column-zero comment: %v", err)
	}
	routes := events.RoutesFromMeta(meta)
	if len(routes) != 1 || routes[0].SinkName != "sample.sink" {
		t.Fatalf("routes = %#v, want one route to sample.sink", routes)
	}
}

func TestDirectiveRejectsDuplicateRouteDestination(t *testing.T) {
	source := "@events {\n" +
		"  observe sample.events.ready -> sample.sink\n" +
		"  observe sample.events.ready -> sample.other_sink\n" +
		"}\n"
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(events.Directive), source, "duplicate.nflow")
	if err == nil || !strings.Contains(err.Error(), "multiple destinations are not supported") {
		t.Fatalf("error = %v, want duplicate destination diagnostic", err)
	}
	position, ok := compiler.PositionOf(err)
	if !ok || position.Line != 3 {
		t.Fatalf("position = %#v, positioned = %v; want line 3", position, ok)
	}
}

func TestRouteChecksTypedSinkAndResolvesAliasedEventSource(t *testing.T) {
	cfg := testConfig(t)
	valid := "@events {\n" +
		"  observe edge.events.ready -> edge.sink\n" +
		"}\n" +
		"{bind_address: \"127.0.0.1:4312\"} -> edge.publish\n"
	if _, err := runner.Compile(t.Context(), cfg, valid, "valid-route.nflow"); err != nil {
		t.Fatalf("valid aliased route did not compile: %v", err)
	}

	invalid := strings.Replace(valid, "edge.sink", "edge.wrong_sink", 1)
	_, err := runner.Compile(t.Context(), cfg, invalid, "invalid-route.nflow")
	if err == nil {
		t.Fatal("event route accepted a sink with the wrong payload type")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "accepts") {
		t.Fatalf("typed sink mismatch error = %v", err)
	}
	position, ok := compiler.PositionOf(err)
	if !ok || position.File != "invalid-route.nflow" || position.Line != 2 {
		t.Fatalf("unknown-field position = %#v, positioned = %v", position, ok)
	}
}

func TestManualConfigUsesEventSpecsFromCompileOptions(t *testing.T) {
	cfg := testConfig(t)
	cfg.EventSpecs = nil
	source := "@events {\n" +
		"  observe edge.events.ready -> edge.sink\n" +
		"}\n" +
		"{bind_address: \"127.0.0.1:4312\"} -> edge.publish\n"
	if _, err := runner.Compile(t.Context(), cfg, source, "manual-config.nflow"); err != nil {
		t.Fatalf("manual Config with event specs in CompileOpts did not compile: %v", err)
	}
}

func TestNestedIncludedEventRoutesComposeWithoutHostPublisher(t *testing.T) {
	childSinkCalls := 0
	childSink := events.NewNonRetainingEventSink[readyPayload]("nested.child_sink", func(_ context.Context, payload *readyPayload) error {
		if payload.BindAddress != "child" {
			return fmt.Errorf("unexpected child payload: %#v", payload)
		}
		childSinkCalls++
		return nil
	})
	parentSink := events.NewNonRetainingEventSink[readyPayload]("nested.parent_sink", func(context.Context, *readyPayload) error { return nil })
	publishChild := action.New("nested.publish_child", func(ctx context.Context, _ struct{}) (string, error) {
		payload := &readyPayload{BindAddress: "child"}
		return "published", events.PublishFromBundle(ctx, "nested", "events.child_ready", payload)
	}).Build()
	bundle := compiler.Bundle{
		ID:    "nested",
		Alias: "edge",
		Libraries: []action.Library{{
			Name:    "nested",
			Actions: []action.AnyAction{publishChild, parentSink, childSink},
		}},
		Events: []compiler.EventSpec{
			{Name: "nested.events.parent_ready", Subject: "events.parent_ready", PayloadType: reflect.TypeFor[readyPayload]()},
			{Name: "nested.events.child_ready", Subject: "events.child_ready", PayloadType: reflect.TypeFor[readyPayload]()},
		},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), bundle))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.nflow")
	childPath := filepath.Join(dir, "child.nflow")
	childSource := `@events {
  observe edge.events.child_ready -> edge.child_sink
}
@pipeline publish_child
  {} -> edge.publish_child
@end
`
	source := `@events {
  observe edge.events.parent_ready -> edge.parent_sink
}
@include "./child.nflow" AS child
child.pipeline.publish_child`
	ctx := compiler.WithSourceLoader(t.Context(), compiler.SourceMap{childPath: childSource})
	if _, err := runner.Execute(ctx, cfg, source, mainPath, nil); err != nil {
		t.Fatalf("execute included child event route without a host publisher: %v", err)
	}
	if childSinkCalls != 1 {
		t.Fatalf("child sink calls = %d, want exactly one delivery", childSinkCalls)
	}
}

func TestRoutePublishesTypedPayloadAndComposesHostPublisherWithoutJournalStep(t *testing.T) {
	var stderr strings.Builder
	sinkCalls := 0
	sink := events.NewNonRetainingEventSink[readyPayload]("sample.sink", func(_ context.Context, payload *readyPayload) error {
		if payload.BindAddress != "127.0.0.1:4312" {
			return fmt.Errorf("unexpected sink payload: %#v", payload)
		}
		sinkCalls++
		_, err := fmt.Fprintln(&stderr, payload.BindAddress)
		return err
	})
	cfg := testConfigWithSink(t, sink)
	journal := &memoryJournal{}
	cfg.Journal = journal
	publisher := &recordingPublisher{}
	source := "@events {\n" +
		"  observe edge.events.ready -> edge.sink\n" +
		"}\n" +
		"{bind_address: \"127.0.0.1:4312\"} -> edge.publish\n"
	ctx := xctx.WithEventPublisher(t.Context(), publisher)
	code := runner.RunSource(ctx, cfg, source, "event-route.nflow", nil, runner.Opts{
		Stdout:      io.Discard,
		Stderr:      &stderr,
		ExecutionID: "event-route-run",
	})
	if code != 0 {
		t.Fatalf("RunSource exit code = %d; stderr: %s", code, stderr.String())
	}
	if got := stderr.String(); !strings.HasPrefix(got, "127.0.0.1:4312\n") || strings.Count(got, "127.0.0.1:4312\n") != 1 || sinkCalls != 1 {
		t.Fatalf("typed sink output = %q, calls = %d; want one payload delivery", got, sinkCalls)
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.subjects) != 1 || publisher.subjects[0] != "events.ready" {
		t.Fatalf("host publisher subjects = %#v", publisher.subjects)
	}
	if publisher.routeInvocation {
		t.Fatal("host publisher received the route-only execution context")
	}
	payload, ok := publisher.payloads[0].(*readyPayload)
	if !ok || payload.BindAddress != "127.0.0.1:4312" {
		t.Fatalf("host publisher payload = %#v", publisher.payloads[0])
	}
	steps := journal.recordedSteps()
	if !slices.Contains(steps, "flow|event-route.nflow|main") {
		t.Fatalf("journal steps = %#v; expected the ordinary top-level Flow step", steps)
	}
	for _, step := range steps {
		if strings.Contains(step, "<events ") || strings.Contains(step, "sample.sink") {
			t.Fatalf("journal steps = %#v; event sink must not add a step", steps)
		}
	}
}

func TestRouteRejectsPublisherFromAnotherBundleBeforeDispatch(t *testing.T) {
	spoof := action.New("attacker.publish", func(ctx context.Context, req emitRequest) (any, error) {
		payload := readyPayload(req)
		return nil, events.PublishFromBundle(ctx, "attacker", "events.ready", &payload)
	}).Build()
	attacker := compiler.Bundle{
		ID:    "attacker",
		Alias: "intruder",
		Libraries: []action.Library{{
			Name:    "attacker",
			Actions: []action.AnyAction{spoof},
		}},
		Events: []compiler.EventSpec{{
			Name:        "attacker.events.started",
			Subject:     "events.started",
			Description: "unrelated attacker event",
			PayloadType: reflect.TypeFor[readyPayload](),
		}},
	}
	cfg := testConfig(t, attacker)
	publisher := &recordingPublisher{}
	var stderr strings.Builder
	source := "@events {\n" +
		"  observe edge.events.ready -> edge.sink\n" +
		"}\n" +
		"{bind_address: \"spoofed\"} -> intruder.publish\n"
	code := runner.RunSource(xctx.WithEventPublisher(t.Context(), publisher), cfg, source, "spoofed-route.nflow", nil, runner.Opts{
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if code == 0 || !strings.Contains(stderr.String(), "not authorized") {
		t.Fatalf("RunSource code = %d, stderr = %q; want producer authorization failure", code, stderr.String())
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.subjects) != 0 {
		t.Fatalf("host delegate received unauthorized subject(s): %#v", publisher.subjects)
	}
}

func TestUnknownEventSourceFailsAtItsDeclarationPosition(t *testing.T) {
	cfg := testConfig(t)
	source := "@events {\n" +
		"  observe edge.events.missing -> edge.sink\n" +
		"}\n"
	_, err := runner.Compile(t.Context(), cfg, source, "unknown-event.nflow")
	if err == nil || !strings.Contains(err.Error(), "unknown event source") {
		t.Fatalf("error = %v, want unknown event source", err)
	}
	position, ok := compiler.PositionOf(err)
	if !ok || position.Line != 2 {
		t.Fatalf("position = %#v, positioned = %v; want line 2", position, ok)
	}
}

func TestRouteRejectsSinkWithMismatchedPayloadType(t *testing.T) {
	sink := events.NewNonRetainingEventSink[readyPayload]("unsupported.sink", func(context.Context, *readyPayload) error {
		return nil
	})
	unsupported := compiler.Bundle{
		ID:    "unsupported",
		Alias: "bad",
		Libraries: []action.Library{{
			Name:    "unsupported",
			Actions: []action.AnyAction{sink},
		}},
		Events: []compiler.EventSpec{{
			Name:        "unsupported.events.payload",
			Subject:     "events.payload",
			Description: "unsupported expression payload shape",
			PayloadType: reflect.TypeFor[unsupportedEventPayload](),
		}},
	}
	cfg, err := runner.BuildConfig(append(native.Bundles(), unsupported))
	if err != nil {
		t.Fatal(err)
	}
	source := "@events {\n" +
		"  observe bad.events.payload -> bad.sink\n" +
		"}\n"
	_, err = runner.Compile(t.Context(), cfg, source, "unsupported-payload.nflow")
	if err == nil || !strings.Contains(err.Error(), "accepts") {
		t.Fatalf("compile error = %v, want typed-sink mismatch diagnostic", err)
	}
	position, ok := compiler.PositionOf(err)
	if !ok || position.Line != 2 {
		t.Fatalf("typed-sink mismatch position = %#v, positioned = %v; want line 2", position, ok)
	}
}

func TestConsoleStderrRequiresAnInjectedWriter(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runner.Compile(t.Context(), cfg, `console.stderr(message: "x")`, "stderr.nflow")
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.ExecuteCompiled(t.Context(), cfg, compiled, `console.stderr(message: "x")`, "stderr.nflow", nil)
	if err == nil || !strings.Contains(err.Error(), "no stderr writer is injected") {
		t.Fatalf("error = %v, want missing injected writer", err)
	}
}

func testConfig(t *testing.T, extras ...compiler.Bundle) runner.Config {
	t.Helper()
	sink := events.NewNonRetainingEventSink[readyPayload]("sample.sink", func(context.Context, *readyPayload) error {
		return nil
	})
	return testConfigWithSink(t, sink, extras...)
}

func testConfigWithSink(t *testing.T, sink action.AnyAction, extras ...compiler.Bundle) runner.Config {
	t.Helper()
	emit := action.New("sample.publish", func(ctx context.Context, req emitRequest) (any, error) {
		payload := readyPayload(req)
		return nil, events.PublishFromBundle(ctx, "sample", "events.ready", &payload)
	}).Build()
	wrongSink := events.NewNonRetainingEventSink[emitRequest]("sample.wrong_sink", func(context.Context, *emitRequest) error {
		return nil
	})
	sample := compiler.Bundle{
		ID:    "sample",
		Alias: "edge",
		Libraries: []action.Library{{
			Name:    "sample",
			Actions: []action.AnyAction{emit, sink, wrongSink},
		}},
		Events: []compiler.EventSpec{{
			Name:        "sample.events.ready",
			Subject:     "events.ready",
			Description: "test readiness fact",
			PayloadType: reflect.TypeFor[readyPayload](),
		}},
	}
	bundles := append(native.Bundles(), sample)
	bundles = append(bundles, extras...)
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
