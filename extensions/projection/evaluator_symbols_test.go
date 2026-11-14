package projection

import (
	"context"
	"reflect"
	"testing"

	compiler "github.com/nexssp/flow/compiler"
)

func TestExprEvaluatorBindsFileLocalSymbols(t *testing.T) {
	ctx := compiler.WithLocalSymbols(context.Background(), map[string]compiler.SymbolNamespace{
		"ap": {
			Name: "ap",
			Symbols: map[string]any{
				"needs_review": "NEEDS_REVIEW",
				"review_queue": "ap-exceptions",
			},
		},
	})

	got, err := NewExprEvaluator()(ctx, Request{
		Raw:   "status: ap.needs_review, queue: ap.review_queue",
		Input: map[string]any{},
	})
	if err != nil {
		t.Fatalf("evaluate projection: %v", err)
	}

	want := map[string]any{
		"status": "NEEDS_REVIEW",
		"queue":  "ap-exceptions",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection = %#v, want %#v", got, want)
	}
}

func TestExprEvaluatorExposesTopLevelMapSnakeCaseAliases(t *testing.T) {
	evaluate := NewExprEvaluator()
	cases := []struct {
		name  string
		input map[string]any
		want  int
	}{
		{name: "single alias", input: map[string]any{"HttpServer": 2}, want: 2},
		{name: "lexically first collision", input: map[string]any{"HttpServer": 2, "HTTPServer": 1}, want: 1},
		{name: "exact key wins", input: map[string]any{"HttpServer": 2, "http_server": 3}, want: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluate(context.Background(), Request{Raw: "value: .http_server", Input: tc.input})
			if err != nil {
				t.Fatalf("evaluate projection: %v", err)
			}
			values, ok := got.(map[string]any)
			if !ok || values["value"] != tc.want {
				t.Fatalf("projection = %#v, want value=%d", got, tc.want)
			}
		})
	}
}
