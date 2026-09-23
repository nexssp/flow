package nodes

import (
	"context"
	"testing"
)

func TestAssert_DotNotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cond  string
		state map[string]any
		ok    bool
	}{
		{`.game_over == false`, map[string]any{"game_over": false}, true},
		{`.game_over == false`, map[string]any{"game_over": true}, false},
		{`.take_val >= 1 && .take_val <= 3`, map[string]any{"take_val": 2}, true},
		{`.take_val >= 1 && .take_val <= 3`, map[string]any{"take_val": 5}, false},
		{`.stones <= 0`, map[string]any{"stones": 0}, true},
		{`.a.b.c > 0`, map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}, true},
		// String literals must not be rewritten.
		{`.version == "v1.2.3"`, map[string]any{"version": "v1.2.3"}, true},
	}

	for _, tc := range cases {
		act, err := NewAssertAction(tc.cond, "")
		if err != nil {
			t.Fatalf("compile %q: %v", tc.cond, err)
		}
		_, runErr := act.Do(context.Background(), tc.state)
		if tc.ok && runErr != nil {
			t.Errorf("%q with %v: want pass, got %v", tc.cond, tc.state, runErr)
		}
		if !tc.ok && runErr == nil {
			t.Errorf("%q with %v: want fail, got pass", tc.cond, tc.state)
		}
	}
}
