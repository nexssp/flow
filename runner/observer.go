package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/compiler/redact"
	"github.com/nexssp/flow/compiler/secret"
)

const observerEventCapacity = 256

var secretScrubberContextKey = xctx.NewKey[*secret.Scrubber]("flow.runner.secret-scrubber")

func activeSecretScrubber(ctx context.Context, fallback *secret.Scrubber) *secret.Scrubber {
	if ctx != nil {
		if scrubber, ok := secretScrubberContextKey.From(ctx); ok && scrubber != nil {
			return scrubber
		}
	}
	return fallback
}

// Observer adapts Kernel lifecycle events to Flow's existing CLI trace while
// retaining a bounded, metadata-only snapshot for callers that need lifecycle
// information without action payloads.
type Observer struct {
	out       io.Writer
	verbosity int

	mu              sync.Mutex
	records         []Record
	events          *observe.MemorySink
	secretContracts *secretContractRegistry
	secretScrubber  *secret.Scrubber
}

type Record struct {
	Action   string
	Duration time.Duration
	Err      error
	Start    time.Time
}

func NewObserver(out io.Writer, verbosity int) *Observer {
	if out == nil {
		out = io.Discard
	}
	return &Observer{
		out:       out,
		verbosity: verbosity,
		events:    observe.NewMemorySink(observerEventCapacity),
	}
}

// Hook returns Kernel's standard lifecycle hook backed by this Flow adapter.
func (o *Observer) Hook() action.AnyHook {
	return observe.Hook(o)
}

// Emit stores a payload-free lifecycle snapshot and translates terminal action
// events into the legacy Flow records used by CLI summaries and assertions.
func (o *Observer) Emit(ctx context.Context, event observe.Event) {
	if o == nil {
		return
	}
	event = sanitizeObservedEvent(event, o.secretContracts, activeSecretScrubber(ctx, o.secretScrubber))
	if o.events != nil {
		o.events.Emit(ctx, metadataOnlyEvent(event))
	}
	switch event.Kind {
	case observe.KindSuccess:
		o.record(event, nil)
	case observe.KindError:
		o.record(event, event.Error)
	}
}

// Events returns an isolated, oldest-to-newest snapshot of the most recent
// Kernel lifecycle metadata. Request, response, error, and recovered values are
// intentionally omitted; callers that explicitly need raw events may provide
// Config.EventSink instead. The snapshot is bounded to observerEventCapacity.
func (o *Observer) Events() []observe.Event {
	if o == nil || o.events == nil {
		return nil
	}
	return o.events.Events()
}

func metadataOnlyEvent(event observe.Event) observe.Event {
	event.Request = nil
	event.Response = nil
	event.Error = nil
	event.Recovered = nil
	return event
}

func (o *Observer) record(event observe.Event, err error) {
	start := event.Time.Add(-event.Duration)
	if event.Time.IsZero() {
		start = time.Now().Add(-event.Duration)
	}
	dur := event.Duration

	o.mu.Lock()
	o.records = append(o.records, Record{
		Action:   event.Action,
		Duration: dur,
		Err:      err,
		Start:    start,
	})
	o.mu.Unlock()

	// -vv prints live progress
	if o.verbosity == 2 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
	}
	// -vvv prints live progress WITH data payloads
	if o.verbosity >= 3 {
		status := "✓"
		if err != nil {
			status = "✗"
		}
		fmt.Fprintf(o.out, "  %s %-30s %s\n", status, event.Action, FormatDuration(dur))
		fmt.Fprintf(o.out, "      in:  %s\n", formatTracePayload(secret.SanitizeWithSources(event.Request, event.Request, event.Response)))
		if err != nil {
			fmt.Fprintf(o.out, "      err: %s\n", formatErrorSummary(err))
		} else {
			fmt.Fprintf(o.out, "      out: %s\n", formatTracePayload(secret.SanitizeWithSources(event.Response, event.Request, event.Response)))
		}
	}
}

func formatTracePayload(v any) string {
	if v == nil {
		return "nil"
	}
	if m, ok := v.(map[string]any); ok {
		v = redact.Map(m)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := string(b)
	if len(s) > 256 {
		return s[:253] + "..."
	}
	return s
}

func (o *Observer) Records() []Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Record, len(o.records))
	copy(out, o.records)
	return out
}

func (o *Observer) ActionNames() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.records))
	for i, r := range o.records {
		out[i] = r.Action
	}
	return out
}

func (o *Observer) PrintSummary(w io.Writer) {
	records := o.Records()
	if len(records) == 0 {
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "─── EXECUTION TRACE ───────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %-6s %-35s %10s   %s\n", "STATUS", "ACTION", "DURATION", "ERROR")

	var total time.Duration
	for _, r := range records {
		status := "✓"
		errStr := "-"
		if r.Err != nil {
			status = "✗"
			errStr = formatErrorSummary(r.Err)
			if len(errStr) > 40 {
				errStr = errStr[:37] + "..."
			}
		}
		fmt.Fprintf(w, "  %-6s %-35s %10s   %s\n", status, r.Action, FormatDuration(r.Duration), errStr)
		total += r.Duration
	}

	fmt.Fprintln(w, "───────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %-6s %-35s %10s\n", "", "TOTAL", FormatDuration(total))
	fmt.Fprintln(w, "")
}

// FormatDuration handles Windows clock resolution limits cleanly without misleading '0ns' outputs.
func FormatDuration(d time.Duration) string {
	if d <= 0 || d < time.Microsecond {
		return "< 1µs"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000.0)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

var _ observe.Sink = (*Observer)(nil)

type secretSanitizingSink struct {
	next            observe.Sink
	secretContracts *secretContractRegistry
	secretScrubber  *secret.Scrubber
}

func (sink secretSanitizingSink) Emit(ctx context.Context, event observe.Event) {
	if sink.next == nil {
		return
	}
	sink.next.Emit(ctx, sanitizeObservedEvent(event, sink.secretContracts, activeSecretScrubber(ctx, sink.secretScrubber)))
}

var _ observe.Sink = secretSanitizingSink{}

type secretCaptureSink struct{}

func (secretCaptureSink) Emit(context.Context, observe.Event) {}

var _ observe.Sink = secretCaptureSink{}

type secretContractRegistry struct {
	mu        sync.RWMutex
	contracts map[string]secretActionContract
}

type secretActionContract struct {
	request  reflect.Type
	response reflect.Type
}

func newSecretContractRegistry() *secretContractRegistry {
	return &secretContractRegistry{contracts: make(map[string]secretActionContract)}
}

func (registry *secretContractRegistry) register(name string, request, response reflect.Type) {
	if registry == nil || name == "" || !secret.TypeContains(request) && !secret.TypeContains(response) {
		return
	}
	registry.mu.Lock()
	registry.contracts[name] = secretActionContract{request: request, response: response}
	registry.mu.Unlock()
}

func (registry *secretContractRegistry) contractTypes(name string) (secretActionContract, bool) {
	if registry == nil {
		return secretActionContract{}, false
	}
	registry.mu.RLock()
	contract, ok := registry.contracts[name]
	registry.mu.RUnlock()
	return contract, ok
}

func sanitizeObservedEvent(event observe.Event, registry *secretContractRegistry, scrubber *secret.Scrubber) observe.Event {
	var wireSecrets []string
	contract, hasContract := registry.contractTypes(event.Action)
	if scrubber != nil {
		var requestType, responseType reflect.Type
		if hasContract {
			requestType, responseType = contract.request, contract.response
		}
		scrubber.Capture(event.Request, requestType)
		scrubber.Capture(event.Response, responseType)
		scrubber.Capture(event.Recovered, nil)
	}
	if hasContract {
		var requestSecrets, responseSecrets []string
		if contract.request != nil {
			event.Request, requestSecrets = secret.SanitizeBoundary(event.Request, contract.request)
		}
		if contract.response != nil {
			event.Response, responseSecrets = secret.SanitizeBoundary(event.Response, contract.response)
		}
		wireSecrets = append(wireSecrets, requestSecrets...)
		wireSecrets = append(wireSecrets, responseSecrets...)
		event.Response = secret.SanitizeWithSecretValues(event.Response, wireSecrets...)
		event.Error = secret.SanitizeErrorWithSecretValues(event.Error, wireSecrets...)
		event.Recovered = secret.SanitizeWithSecretValues(event.Recovered, wireSecrets...)
	}
	sources := []any{event.Request, event.Response, event.Recovered}
	event.Request = secret.SanitizeWithSources(event.Request, sources...)
	event.Response = secret.SanitizeWithSources(event.Response, sources...)
	event.Error = secret.SanitizeError(event.Error, sources...)
	if recoveredErr, ok := event.Recovered.(error); ok {
		event.Recovered = secret.SanitizeError(recoveredErr, sources...)
	} else {
		event.Recovered = secret.SanitizeWithSources(event.Recovered, sources...)
	}
	if scrubber != nil {
		event.Request = scrubber.Sanitize(event.Request)
		event.Response = scrubber.Sanitize(event.Response)
		event.Error = scrubber.SanitizeError(event.Error)
		if recoveredErr, ok := event.Recovered.(error); ok {
			event.Recovered = scrubber.SanitizeError(recoveredErr)
		} else {
			event.Recovered = scrubber.Sanitize(event.Recovered)
		}
	}
	return event
}

func sanitizeHookEvent(ctx context.Context, event observe.Event, registry *secretContractRegistry, scrubber *secret.Scrubber) observe.Event {
	return sanitizeObservedEvent(event, registry, activeSecretScrubber(ctx, scrubber))
}

func secretSanitizingHook(hook action.AnyHook, registry *secretContractRegistry, scrubber *secret.Scrubber) action.AnyHook {
	safe := hook
	if hook.Before != nil {
		safe.Before = func(ctx context.Context, request any, meta *action.Meta) (context.Context, error) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			next, err := hook.Before(ctx, event.Request, meta)
			event.Error = err
			return next, sanitizeHookEvent(ctx, event, registry, scrubber).Error
		}
	}
	if hook.After != nil {
		safe.After = func(ctx context.Context, request, response any, err error, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Response: response, Error: err}, registry, scrubber)
			hook.After(ctx, event.Request, event.Response, event.Error, meta)
		}
	}
	if hook.OnSuccess != nil {
		safe.OnSuccess = func(ctx context.Context, request, response any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Response: response}, registry, scrubber)
			hook.OnSuccess(ctx, event.Request, event.Response, meta)
		}
	}
	if hook.OnTimeout != nil {
		safe.OnTimeout = func(ctx context.Context, request any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			hook.OnTimeout(ctx, event.Request, meta)
		}
	}
	if hook.OnError != nil {
		safe.OnError = func(ctx context.Context, request any, err error, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Error: err}, registry, scrubber)
			hook.OnError(ctx, event.Request, event.Error, meta)
		}
	}
	if hook.OnRetry != nil {
		safe.OnRetry = func(ctx context.Context, request any, attempt int, err error, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Error: err}, registry, scrubber)
			hook.OnRetry(ctx, event.Request, attempt, event.Error, meta)
		}
	}
	if hook.OnCacheHit != nil {
		safe.OnCacheHit = func(ctx context.Context, request, response any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Response: response}, registry, scrubber)
			hook.OnCacheHit(ctx, event.Request, event.Response, meta)
		}
	}
	if hook.OnCacheMiss != nil {
		safe.OnCacheMiss = func(ctx context.Context, request any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			hook.OnCacheMiss(ctx, event.Request, meta)
		}
	}
	if hook.OnCoalesced != nil {
		safe.OnCoalesced = func(ctx context.Context, request any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			hook.OnCoalesced(ctx, event.Request, meta)
		}
	}
	if hook.OnDeduplicated != nil {
		safe.OnDeduplicated = func(ctx context.Context, request any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			hook.OnDeduplicated(ctx, event.Request, meta)
		}
	}
	if hook.OnCancel != nil {
		safe.OnCancel = func(ctx context.Context, request any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request}, registry, scrubber)
			hook.OnCancel(ctx, event.Request, meta)
		}
	}
	if hook.OnPanic != nil {
		safe.OnPanic = func(ctx context.Context, request, recovered any, meta *action.Meta) {
			event := sanitizeHookEvent(ctx, observe.Event{Action: hookActionName(meta), Request: request, Recovered: recovered}, registry, scrubber)
			hook.OnPanic(ctx, event.Request, event.Recovered, meta)
		}
	}
	return safe
}

func hookActionName(meta *action.Meta) string {
	if meta == nil {
		return ""
	}
	return meta.Name
}
