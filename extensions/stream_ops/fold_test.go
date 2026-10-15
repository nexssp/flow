package stream_ops_test

import (
	"bytes"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/cli/selftest"
	compiler "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/stream_ops"
)

func TestStreamOps_NFlowFixtures(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	code := selftest.Run(t.Context(), selftest.Options{
		Out:     &output,
		Filters: []string{"stream_ops"},
	})
	if code != 0 {
		t.Fatalf("stream_ops self-test exit code = %d; output:\n%s", code, output.String())
	}
}

func TestStreamFold_AggregatesRecordFields(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	foldOperator, ok := resolver.Operator("stream.fold")
	ktest.RequireTrue(t, ok)

	built, err := foldOperator.Build(map[string]any{
		"initial":    map[string]any{"count": 0, "total": 0},
		"expression": "{ count: acc.count + 1, total: acc.total + item.size }",
	})
	ktest.RequireNoError(t, err)

	items := []any{
		map[string]any{"size": 4},
		map[string]any{"size": 7},
		map[string]any{"size": 9},
	}
	source := streamSource(items...)
	upstream, err := source.DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)
	folded, err := built.Apply(upstream)
	ktest.RequireNoError(t, err)

	var results []any
	var streamErrors []error
	folded(func(item any, itemErr error) bool {
		if itemErr != nil {
			streamErrors = append(streamErrors, itemErr)
			return true
		}
		results = append(results, item)
		return true
	})

	ktest.RequireEqual(t, len(streamErrors), 0)
	ktest.RequireEqual(t, len(results), 1)
	stats, ok := results[0].(map[string]any)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, numberAsInt(t, stats["count"]), 3)
	ktest.RequireEqual(t, numberAsInt(t, stats["total"]), 20)
}

func TestStreamFold_EmptyStreamEmitsInitialValue(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	foldOperator, ok := resolver.Operator("stream.fold")
	ktest.RequireTrue(t, ok)
	built, err := foldOperator.Build(map[string]any{
		"initial":    map[string]any{"count": 0},
		"expression": "{ count: acc.count + 1 }",
	})
	ktest.RequireNoError(t, err)

	upstream, err := streamSource().DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)
	folded, err := built.Apply(upstream)
	ktest.RequireNoError(t, err)

	var results []any
	folded(func(item any, itemErr error) bool {
		ktest.RequireNoError(t, itemErr)
		results = append(results, item)
		return true
	})

	ktest.RequireEqual(t, len(results), 1)
	stats, ok := results[0].(map[string]any)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, numberAsInt(t, stats["count"]), 0)
}

func TestStreamFold_InvalidExpressionEmitsError(t *testing.T) {
	t.Parallel()

	resolver, err := compiler.NewDynamicResolver(stream_ops.Bundle(nil).Libraries...)
	ktest.RequireNoError(t, err)
	foldOperator, ok := resolver.Operator("stream.fold")
	ktest.RequireTrue(t, ok)
	built, err := foldOperator.Build(map[string]any{
		"initial":    0,
		"expression": "acc +",
	})
	ktest.RequireNoError(t, err)
	upstream, err := streamSource().DoStreamAny(t.Context(), struct{}{})
	ktest.RequireNoError(t, err)
	folded, err := built.Apply(upstream)
	ktest.RequireNoError(t, err)

	var gotErr error
	folded(func(_ any, err error) bool {
		gotErr = err
		return true
	})
	ktest.RequireErrorContains(t, gotErr, "stream.fold: invalid expression")
}

func numberAsInt(t *testing.T, value any) int {
	t.Helper()
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		t.Fatalf("got %T (%v), want numeric value", value, value)
		return 0
	}
}
