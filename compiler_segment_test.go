package flow_test

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/stream"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// testSource is a minimal stream source used by the segmented-compiler
// tests. It ignores its request and yields a fixed slice of strings.
func testSource(name string, items ...string) action.AnyStreamAction {
	cp := append([]string(nil), items...)
	return action.NewStream[struct{}, string](name,
		func(_ context.Context, _ struct{}) (iter.Seq2[string, error], error) {
			return func(yield func(string, error) bool) {
				for _, item := range cp {
					if !yield(item, nil) {
						return
					}
				}
			}, nil
		},
	)
}

// testOperator is a passthrough operator that applies fn to each item.
func testOperator(name string, fn func(string) string) action.NamedOperator {
	return action.NewOperator(name,
		func(_ struct{}) stream.StreamOp[string, string] {
			return func(up iter.Seq2[string, error]) iter.Seq2[string, error] {
				return func(yield func(string, error) bool) {
					for item, err := range up {
						if err != nil {
							var zero string
							yield(zero, err)
							return
						}
						if !yield(fn(item), nil) {
							return
						}
					}
				}
			}
		},
	)
}

// buildRegistries wires a minimal flow registry (one source, one
// operator) and a kernel registry (one observer action). Together they
// cover every atom a segmented pipeline test needs.
func buildRegistries(
	t *testing.T,
	src action.AnyStreamAction,
	op action.NamedOperator,
) (*flow.Registry, *action.Registry) {
	t.Helper()

	fr := flow.NewRegistry()
	err := fr.Register(flow.Library{
		Name:      "test",
		Sources:   []action.AnyStreamAction{src},
		Operators: []action.NamedOperator{op},
	})
	if err != nil {
		t.Fatalf("register flow lib: %v", err)
	}

	observe := action.New("test.observe",
		func(_ context.Context, req any) (map[string]any, error) {
			items, _ := req.([]any)
			joined := make([]string, 0, len(items))
			for _, it := range items {
				joined = append(joined, fmt.Sprint(it))
			}
			return map[string]any{
				"count": len(items),
				"items": joined,
			}, nil
		},
	).Build()

	kr, err := action.NewRegistry(action.Library{
		Name:    "test",
		Actions: []action.AnyAction{observe},
	})
	if err != nil {
		t.Fatalf("register kernel lib: %v", err)
	}

	return fr, kr
}

// compileAndRun is the standard test driver: it compiles the given DSL
// with the given registries, runs it against nil, and returns the raw
// result. It fails the test on any error.
func compileAndRun(
	t *testing.T,
	dsl string,
	fr *flow.Registry,
	kr *action.Registry,
) any {
	t.Helper()

	bld, err := flow.CompilePipeline(dsl, kr, flow.WithFlowRegistry(fr))
	if err != nil {
		t.Fatalf("compile %q: %v", dsl, err)
	}

	out, err := bld.Build().Do(context.Background(), nil)
	if err != nil {
		t.Fatalf("run %q: %v", dsl, err)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy path
// ─────────────────────────────────────────────────────────────────────────────

func TestSegmented_HappyPath_StreamCollectUnary(t *testing.T) {
	src := testSource("test.walk", "a.go", "b.go", "c.go")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	out := compileAndRun(t, `test.walk -> test.upper -> collect -> test.observe`, fr, kr)

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T (%v)", out, out)
	}
	if result["count"] != 3 {
		t.Fatalf("count = %v, want 3", result["count"])
	}

	items, _ := result["items"].([]string)
	if len(items) != 3 {
		t.Fatalf("items = %v, want 3 elements", items)
	}
	if items[0] != "A.GO" {
		t.Fatalf("items[0] = %q, want %q", items[0], "A.GO")
	}
}

func TestSegmented_EmptySource(t *testing.T) {
	src := testSource("test.walk") // no items
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	out := compileAndRun(t, `test.walk -> test.upper -> collect -> test.observe`, fr, kr)

	result := out.(map[string]any)
	if result["count"] != 0 {
		t.Fatalf("count = %v, want 0", result["count"])
	}
}

func TestSegmented_TerminalBoundary(t *testing.T) {
	// Pipeline ends at the boundary: the caller receives the slice.
	src := testSource("test.walk", "a", "b")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	out := compileAndRun(t, `test.walk -> test.upper -> collect`, fr, kr)

	items, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestSegmented_Error_BoundaryAtStart(t *testing.T) {
	src := testSource("test.walk", "a")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	_, err := flow.CompilePipeline(
		`collect -> test.observe`,
		kr,
		flow.WithFlowRegistry(fr),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot appear at the start") {
		t.Fatalf("error message = %q, expected 'cannot appear at the start'", err)
	}
}

func TestSegmented_Error_TwoBoundaries(t *testing.T) {
	src := testSource("test.walk", "a")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	_, err := flow.CompilePipeline(
		`test.walk -> collect -> test.upper -> collect`,
		kr,
		flow.WithFlowRegistry(fr),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "only one stream boundary") {
		t.Fatalf("error message = %q, expected 'only one stream boundary'", err)
	}
}

func TestSegmented_Error_UnaryHead(t *testing.T) {
	// The pre-boundary segment must be a valid stream source chain.
	// Starting with a unary atom makes the stream compile fail with a
	// clear message.
	src := testSource("test.walk", "a")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	_, err := flow.CompilePipeline(
		`test.observe -> collect`,
		kr,
		flow.WithFlowRegistry(fr),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The specific error comes from resolveSourceAtom; we only assert
	// that some error mentioning either the atom or the source chain
	// is returned.
	msg := err.Error()
	if !strings.Contains(msg, "test.observe") && !strings.Contains(msg, "stream") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Detection
// ─────────────────────────────────────────────────────────────────────────────

func TestDetectPipelineMode_BoundaryWinsOverStream(t *testing.T) {
	// A pipeline that would otherwise be pure stream must be classified
	// as segmented once a boundary appears.
	src := testSource("test.walk", "a")
	op := testOperator("test.upper", strings.ToUpper)
	fr, kr := buildRegistries(t, src, op)

	// Compile the segmented variant and confirm that the output is the
	// unary observer's map, not the stream drain count.
	out := compileAndRun(t, `test.walk -> collect -> test.observe`, fr, kr)
	if _, ok := out.(map[string]any); !ok {
		t.Fatalf("segmented pipeline produced %T, expected map[string]any", out)
	}
}
