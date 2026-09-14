package nodes_test

import (
	"context"
	"testing"

	"github.com/nexssp/flow/nodes"
	"github.com/nexssp/kernel/action"
)

func TestLogNodes_PassThroughUnchanged(t *testing.T) {
	nodesUnderTest := []action.AnyAction{
		nodes.NewLogInfoAction(),
		nodes.NewLogWarnAction(),
		nodes.NewLogErrorAction(),
	}
	for _, n := range nodesUnderTest {
		in := map[string]any{"message": "hi", "k": "v", "n": 42}

		raw, err := action.InvokeAny(context.Background(), n, in)
		if err != nil {
			t.Fatalf("%s: %v", n.Describe().Name, err)
		}

		out, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s: unexpected return type %T", n.Describe().Name, raw)
		}

		if out["message"] != "hi" || out["k"] != "v" || out["n"] != 42 {
			t.Fatalf("%s: pass-through broken: %+v", n.Describe().Name, out)
		}
	}
}

func TestLogNodes_MissingMessageDoesNotPanic(t *testing.T) {
	_, err := action.InvokeAny(context.Background(), nodes.NewLogInfoAction(),
		map[string]any{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
}
