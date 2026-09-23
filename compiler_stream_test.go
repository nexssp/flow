package flow_test

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
)

// ── Test doubles for stream pipeline compilation ─────────────────────────────

type stringSource struct {
	name  string
	items []any
}

func (s *stringSource) Describe() *action.Meta                                  { return &action.Meta{Name: s.name} }
func (s *stringSource) GetBindings() []action.Binding                           { return nil }
func (s *stringSource) ReqPayload() any                                         { return struct{}{} }
func (s *stringSource) ResPayload() any                                         { return nil }
func (s *stringSource) GetAnyHooks() []action.AnyHook                           { return nil }
func (s *stringSource) AddAnyHook(...action.AnyHook)                            {}
func (s *stringSource) CloneWithHooks(...action.AnyHook) action.AnyStreamAction { return s }

func (s *stringSource) DoStreamAny(_ context.Context, _ any) (action.AnyStream, error) {
	return func(yield func(any, error) bool) {
		for _, item := range s.items {
			if !yield(item, nil) {
				return
			}
		}
	}, nil
}

type keepTestsConfig struct {
	KeepTests bool `json:"keep_tests"`
}

func keepTestsOperator(name string) action.NamedOperator {
	return action.NewOperator(name, func(cfg keepTestsConfig) action.StreamOp[any, any] {
		return func(up iter.Seq2[any, error]) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for item, err := range up {
					if err != nil {
						yield(nil, err)
						return
					}
					s, _ := item.(string)
					if !cfg.KeepTests && strings.HasSuffix(s, "_test") {
						continue
					}
					if !yield(item, nil) {
						return
					}
				}
			}
		}
	})
}

func upperOperator(name string) action.NamedOperator {
	return action.NewSimpleOperator(name, func(up iter.Seq2[any, error]) iter.Seq2[any, error] {
		return up
	})
}

func buildStreamRegistry(t *testing.T, items []any) (*flow.Registry, *action.Registry) {
	t.Helper()
	reg := flow.NewRegistry()
	err := reg.Register(flow.Library{
		Name:    "test",
		Sources: []action.AnyStreamAction{&stringSource{name: "src", items: items}},
		Operators: []action.NamedOperator{
			keepTestsOperator("filter"),
			upperOperator("upper"),
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg, action.MustNewRegistry()
}

// ── Happy paths ──────────────────────────────────────────────────────────────

func TestCompileStream_SourceOnly(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a", "b", "c"})

	bld, err := flow.CompilePipeline("src", kernelReg, flow.WithFlowRegistry(reg))
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	got, err := bld.Build().DoAny(context.Background(), nil)
	if err != nil {
		t.Fatalf("DoAny: %v", err)
	}

	dr, ok := got.(flow.StreamDrainResult)
	if !ok {
		t.Fatalf("expected flow.StreamDrainResult, got %T", got)
	}
	if dr.Count != 3 {
		t.Fatalf("expected Count=3, got %d", dr.Count)
	}
}

func TestCompileStream_SourceThenOneOperator(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a", "b_test", "c"})

	bld, err := flow.CompilePipeline(
		"src -> filter",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	got, err := bld.Build().DoAny(context.Background(), nil)
	if err != nil {
		t.Fatalf("DoAny: %v", err)
	}

	dr, ok := got.(flow.StreamDrainResult)
	if !ok {
		t.Fatalf("expected flow.StreamDrainResult, got %T", got)
	}
	if dr.Count != 2 {
		t.Fatalf("expected Count=2 (a, c), got %d", dr.Count)
	}
}

func TestCompileStream_SourceThenTwoOperators(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a", "b_test", "c", "d_test"})

	bld, err := flow.CompilePipeline(
		"src -> filter -> upper",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	got, err := bld.Build().DoAny(context.Background(), nil)
	if err != nil {
		t.Fatalf("DoAny: %v", err)
	}

	dr, ok := got.(flow.StreamDrainResult)
	if !ok {
		t.Fatalf("expected flow.StreamDrainResult, got %T", got)
	}
	if dr.Count != 2 {
		t.Fatalf("expected Count=2, got %d", dr.Count)
	}
}

func TestCompileStream_OperatorParamFromModifier(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a", "b_test", "c"})

	bld, err := flow.CompilePipeline(
		"src -> filter:keep_tests=true",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err != nil {
		t.Fatalf("CompilePipeline: %v", err)
	}

	got, err := bld.Build().DoAny(context.Background(), nil)
	if err != nil {
		t.Fatalf("DoAny: %v", err)
	}

	dr, ok := got.(flow.StreamDrainResult)
	if !ok {
		t.Fatalf("expected flow.StreamDrainResult, got %T", got)
	}
	if dr.Count != 3 {
		t.Fatalf("expected Count=3 (keep_tests=true), got %d", dr.Count)
	}
}

// ── Rejections ───────────────────────────────────────────────────────────────

func TestCompileStream_RejectsUnaryInStreamPipeline(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a"})

	if err := reg.Register(flow.Library{
		Name: "extra",
		Actions: []action.AnyAction{
			action.New("say", func(_ context.Context, _ any) (string, error) {
				return "", nil
			}).Build(),
		},
	}); err != nil {
		t.Fatalf("register say: %v", err)
	}

	_, err := flow.CompilePipeline(
		"src -> say",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err == nil {
		t.Fatal("expected error mixing stream source with unary action")
	}
	msg := err.Error()
	for _, want := range []string{"say", "unary", "stream"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %q", want, msg)
		}
	}
}

func TestCompileStream_RejectsSourceAfterOperator(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a"})

	if err := reg.Register(flow.Library{
		Name:    "extra",
		Sources: []action.AnyStreamAction{&stringSource{name: "src2"}},
	}); err != nil {
		t.Fatalf("register src2: %v", err)
	}

	_, err := flow.CompilePipeline(
		"src -> filter -> src2",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err == nil {
		t.Fatal("expected error: second source in same pipeline")
	}
	if !strings.Contains(err.Error(), "src2") {
		t.Errorf("error should name src2: %q", err.Error())
	}
}

func TestCompileStream_RejectsOperatorAsHead(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a"})

	_, err := flow.CompilePipeline(
		"filter",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err == nil {
		t.Fatal("expected error: operator cannot start a pipeline")
	}
	if !strings.Contains(err.Error(), "filter") ||
		!strings.Contains(err.Error(), "cannot start") {
		t.Errorf("unexpected error: %q", err.Error())
	}
}

func TestCompileStream_RejectsUnknownOperatorParam(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a"})

	_, err := flow.CompilePipeline(
		"src -> filter:bogus=42",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err == nil {
		t.Fatal("expected error: unknown operator parameter")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name param: %q", err.Error())
	}
}

func TestCompilePipeline_UnaryPath_UnaffectedByFlowRegistry(t *testing.T) {
	t.Parallel()
	reg, kernelReg := buildStreamRegistry(t, []any{"a"})

	_, err := flow.CompilePipeline("unknown.atom", kernelReg)
	if err == nil {
		t.Fatal("expected error for unknown atom without flow registry")
	}

	_, err = flow.CompilePipeline(
		"unknown.atom",
		kernelReg,
		flow.WithFlowRegistry(reg),
	)
	if err == nil {
		t.Fatal("expected error for unknown atom")
	}
}
