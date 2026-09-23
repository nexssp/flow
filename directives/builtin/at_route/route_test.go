package at_route_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/contracts"
	_ "github.com/nexssp/flow/directives/builtin"
	"github.com/nexssp/kernel/action"
)

func TestRoute_EndToEnd(t *testing.T) {
	src := `@route classify {
  when score >= 90 -> grade.a
  when score >= 70 -> grade.b
  else             -> grade.f
}
classify
`

	pre, err := flow.PreprocessBytes([]byte(src), "route_test.nflow")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}

	base := action.MustNewRegistry(action.Of(
		action.New("grade.a", func(_ context.Context, _ map[string]any) (string, error) {
			return "A", nil
		}).Build(),
		action.New("grade.b", func(_ context.Context, _ map[string]any) (string, error) {
			return "B", nil
		}).Build(),
		action.New("grade.f", func(_ context.Context, _ map[string]any) (string, error) {
			return "F", nil
		}).Build(),
	))

	reg, err := flow.ContributeRegistry(pre, base)
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	if _, ok := reg.Get("classify"); !ok {
		t.Fatal("classify action not registered after ContributeRegistry")
	}

	bld, err := flow.CompilePipeline(pre.DSL, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	built := bld.Build()

	ctx := contracts.WithRegistry(context.Background(), reg)

	cases := []struct {
		score float64
		want  string
	}{
		{95, "A"},
		{75, "B"},
		{50, "F"},
		{90, "A"},
		{70, "B"},
		{69, "F"},
	}
	for _, tc := range cases {
		got, err := built.Do(ctx, map[string]any{"score": tc.score})
		if err != nil {
			t.Fatalf("score=%v: %v", tc.score, err)
		}
		if got != tc.want {
			t.Errorf("score=%v: got %v, want %v", tc.score, got, tc.want)
		}
	}
}

func TestRoute_DirectiveErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{"missing_name", "@route {\n  when a -> b\n}\n", "missing name"},
		{"empty_when", "@route x {\n  when a ->\n}\n", "empty condition or target"},
		{"no_arrow", "@route x {\n  when a b\n}\n", "expected `when COND -> TARGET`"},
		{"no_whens", "@route x {\n  else -> b\n}\n", "needs at least one"},
		{
			"duplicate_name",
			"@route x { when a -> b }\n@route x { when c -> d }\n",
			"duplicate declaration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := flow.PreprocessBytes([]byte(tc.src), "bad.nflow")
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}
