package loop_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
	loopext "github.com/nexssp/flow/extensions/loop"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestEachExecutableFixtures(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixtures := loopext.Bundle(nil).Fixtures
	for _, name := range []string{
		"each_ordered_scope.nflow",
		"each_empty.nflow",
		"each_action_error.nflow",
		"each_scoped_modifier.nflow",
	} {
		t.Run(name, func(t *testing.T) {
			source, readErr := fs.ReadFile(fixtures, "nflows/"+name)
			if readErr != nil {
				t.Fatalf("read fixture: %v", readErr)
			}
			if _, runErr := runner.Execute(t.Context(), cfg, string(source), name, nil); runErr != nil {
				t.Fatalf("execute fixture: %v", runErr)
			}
		})
	}
}

func TestEachStaticInvalidInputFixture(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixture, err := fs.ReadFile(loopext.Bundle(nil).Fixtures, "nflows/each_invalid_input.nflow")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	const marker = "# @expect-compile-error "
	var want string
	for line := range strings.SplitSeq(string(fixture), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), marker); ok {
			want = strings.TrimSpace(value)
			break
		}
	}
	if want == "" {
		t.Fatal("fixture has no @expect-compile-error marker")
	}
	if _, err := runner.Compile(t.Context(), cfg, string(fixture), "each_invalid_input.nflow"); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("compile error = %v, want diagnostic containing %q", err, want)
	}
}

func TestEachAcceptsTypedSliceAndRejectsDynamicScalar(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	const source = `pick ( field: "items" ) -> each { noop }`
	result, err := runner.Execute(t.Context(), cfg, source, "typed_slice.nflow", map[string]any{
		"items": []int{2, 4, 6},
	})
	if err != nil {
		t.Fatalf("execute typed-slice input: %v", err)
	}
	values, ok := result.Output.([]any)
	if !ok || len(values) != 3 || values[0] != 2 || values[1] != 4 || values[2] != 6 {
		t.Fatalf("typed-slice output = %#v, want []any{2, 4, 6}", result.Output)
	}

	_, err = runner.Execute(t.Context(), cfg, `const 42 -> each { noop }`, "dynamic_scalar.nflow", nil)
	if err == nil || xerr.KindFrom(err) != xerr.KindValidation || !strings.Contains(err.Error(), "each: input must be a list") {
		t.Fatalf("dynamic scalar error = %v, want each list validation error", err)
	}
}

func TestEachRejectsNilInput(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	_, err = runner.Execute(t.Context(), cfg, `const null -> each { noop }`, "nil_input.nflow", nil)
	if err == nil || xerr.KindFrom(err) != xerr.KindValidation || !strings.Contains(err.Error(), "each: input must be a list, got nil") {
		t.Fatalf("nil input error = %v, want each nil-list validation error", err)
	}
}

func TestEachRejectsListOverLimit(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	items := make([]any, 100_001)
	_, err = runner.Execute(t.Context(), cfg, `pick ( field: "items" ) -> each { noop }`, "over_limit.nflow", map[string]any{
		"items": items,
	})
	if err == nil || xerr.KindFrom(err) != xerr.KindBadRequest || !strings.Contains(err.Error(), "100001 items exceeds limit of 100000") {
		t.Fatalf("over-limit error = %v, want classified error with actual and maximum counts", err)
	}
}

func TestEachBoundsConcurrencyAndPreservesInputOrder(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	firstStarted := make(chan struct{})
	secondCompleted := make(chan struct{})
	var completeSecond sync.Once

	worker := action.New("test.each_worker", func(ctx context.Context, input any) (any, error) {
		current := active.Add(1)
		for previous := maximum.Load(); current > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)

		switch input {
		case 1:
			close(firstStarted)
			select {
			case <-secondCompleted:
				return "first", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case 2:
			select {
			case <-firstStarted:
				return "second", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return "third", nil
		}
	}).Build()
	target := action.Dynamic(worker).HookAfter(func(_ context.Context, request, _ any, err error, _ *action.Meta) {
		if request == 2 && err == nil {
			completeSecond.Do(func() { close(secondCompleted) })
		}
	}).Build()
	cfg := buildEachTestConfig(t, target)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := runner.Execute(ctx, cfg, `pick ( field: "items" ) -> each :concurrency=2 { test.each_worker }`, "ordered_concurrency.nflow", map[string]any{
		"items": []any{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("execute ordered concurrent each: %v", err)
	}
	values, ok := result.Output.([]any)
	if !ok || len(values) != 3 || values[0] != "first" || values[1] != "second" || values[2] != "third" {
		t.Fatalf("output = %#v, want [first second third]", result.Output)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent workers = %d, want 2", got)
	}
}

func TestEachFailureCancelsSiblingsAndWaitsForCleanup(t *testing.T) {
	failure := errors.New("sample action failed")
	secondStarted := make(chan struct{})
	secondStopped := make(chan struct{})
	var thirdStarted atomic.Bool

	worker := action.New("test.each_failure", func(ctx context.Context, input any) (any, error) {
		switch input {
		case 1:
			select {
			case <-secondStarted:
				return nil, failure
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case 2:
			close(secondStarted)
			<-ctx.Done()
			close(secondStopped)
			return nil, ctx.Err()
		default:
			thirdStarted.Store(true)
			return "third", nil
		}
	}).Build()
	cfg := buildEachTestConfig(t, worker)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := runner.Execute(ctx, cfg, `pick ( field: "items" ) -> each :concurrency=2 { test.each_failure }`, "cancel_siblings.nflow", map[string]any{
		"items": []any{1, 2, 3},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("execution error = %v, want original worker error", err)
	}
	select {
	case <-secondStopped:
	default:
		t.Fatal("each returned before the launched sibling cleaned up")
	}
	if thirdStarted.Load() {
		t.Fatal("each started a queued item after fail-fast cancellation")
	}
}

func buildEachTestConfig(t *testing.T, actions ...action.AnyAction) runner.Config {
	t.Helper()
	bundles := native.SelftestBundles()
	bundles = append(bundles, compiler.Bundle{
		ID:        "loop_each_test",
		Libraries: []action.Library{{Name: "loop_each_test", Actions: actions}},
	})
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		t.Fatalf("build each test config: %v", err)
	}
	return cfg
}
