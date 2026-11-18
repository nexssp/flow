package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/internal/constinput"
)

func TestConstDirectObjectTreatsLegacyKeysAsOrdinaryData(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"value":  "42",
		"val":    int64(7),
		"coerce": true,
		"nested": map[string]any{"reference": constinput.String("3")},
	}
	got, err := action.InvokeAny(context.Background(), Const, constinput.Direct{Value: input})
	if err != nil {
		t.Fatalf("invoke const: %v", err)
	}
	want := map[string]any{
		"value":  "42",
		"val":    int64(7),
		"coerce": true,
		"nested": map[string]any{"reference": "3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("const output = %#v, want %#v", got, want)
	}
}
