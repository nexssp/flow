package projection

import (
	"context"
	"reflect"
	"testing"

	core "github.com/nexssp/flow/compiler"
)

func TestExprEvaluatorBindsFileLocalSymbols(t *testing.T) {
	ctx := core.WithLocalSymbols(context.Background(), map[string]core.SymbolNamespace{
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
