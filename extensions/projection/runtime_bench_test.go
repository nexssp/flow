package projection

import (
	"context"
	"fmt"
	"testing"

	"github.com/nexssp/kernel/action"
)

func BenchmarkProjectionActionInjectedRoot(b *testing.B) {
	const raw = "name: .name, selected: .nested.selected.value"
	root := map[string]any{
		"name":   "Julia",
		"nested": map[string]any{"selected": map[string]any{"value": 42}, "unused": "ignored"},
	}
	for index := range 32 {
		root[fmt.Sprintf("unused_%d", index)] = index
	}
	input := map[string]any{"raw": raw, expressionRootName: root}
	project := projectionAction(NewExprEvaluator())
	ctx := context.Background()
	if _, err := action.InvokeAny(ctx, project, input); err != nil {
		b.Fatalf("warmup projection: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := action.InvokeAny(ctx, project, input); err != nil {
			b.Fatal(err)
		}
	}
}
