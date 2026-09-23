package nodes

import (
	"context"
	"strings"
	"testing"
)

// runProjection compiles body, executes it against input, and returns
// the resulting map. It fatals on any error, so individual tests can
// focus on assertions.
func runProjection(t *testing.T, body string, input any) map[string]any {
	t.Helper()

	act, err := NewProjectionAction(body)
	if err != nil {
		t.Fatalf("compile %q: %v", body, err)
	}

	out, err := act.Do(context.Background(), input)
	if err != nil {
		t.Fatalf("exec %q: %v", body, err)
	}

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("exec %q: expected map[string]any, got %T", body, out)
	}

	return m
}

// -----------------------------------------------------------------------------
// Plain projections (no spread)
// -----------------------------------------------------------------------------

func TestProjection_PlainProducesExactFields(t *testing.T) {
	got := runProjection(t, "a: 1, b: 2", map[string]any{
		"a":      99,
		"b":      99,
		"extra":  "dropped",
		"hidden": true,
	})

	if got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("got %+v", got)
	}
	if _, ok := got["extra"]; ok {
		t.Fatalf("plain projection must not carry unrelated fields, got %+v", got)
	}
	if _, ok := got[rootVar]; ok {
		t.Fatalf("%s leaked into output", rootVar)
	}
	if _, ok := got[stateVar]; ok {
		t.Fatalf("%s leaked into output", stateVar)
	}
}

// -----------------------------------------------------------------------------
// Spread projections
// -----------------------------------------------------------------------------

func TestProjection_SpreadPreservesFields(t *testing.T) {
	got := runProjection(t, "..., attempt: attempt + 1", map[string]any{
		"attempt": 1,
		"goal":    "do something",
		"extra":   true,
	})

	if got["attempt"] != 2 {
		t.Fatalf("attempt = %v, want 2", got["attempt"])
	}
	if got["goal"] != "do something" {
		t.Fatalf("goal lost: %+v", got)
	}
	if got["extra"] != true {
		t.Fatalf("extra lost: %+v", got)
	}
	if _, ok := got[stateVar]; ok {
		t.Fatalf("%s leaked into output", stateVar)
	}
	if _, ok := got[rootVar]; ok {
		t.Fatalf("%s leaked into output", rootVar)
	}
}

func TestProjection_PureSpreadIsPassThrough(t *testing.T) {
	in := map[string]any{"a": 1, "b": "x", "c": []int{1, 2, 3}}
	got := runProjection(t, "...", in)

	if got["a"] != 1 || got["b"] != "x" {
		t.Fatalf("got %+v", got)
	}
	if _, ok := got[stateVar]; ok {
		t.Fatalf("%s leaked into output", stateVar)
	}
}

func TestProjection_SpreadOnNonMapDegradesToOverride(t *testing.T) {
	// Spread over a non-map input must not panic; it should behave as
	// "start from an empty map and apply the override".
	got := runProjection(t, "..., a: 1", "hello")

	if got["a"] != 1 {
		t.Fatalf("a = %v, want 1", got["a"])
	}
}

func TestProjection_SpreadWithExpression(t *testing.T) {
	got := runProjection(t, "..., total: total + 10", map[string]any{
		"total": 5,
		"name":  "counter",
	})

	if got["total"] != 15 {
		t.Fatalf("total = %v, want 15", got["total"])
	}
	if got["name"] != "counter" {
		t.Fatalf("name lost: %+v", got)
	}
}

func TestProjection_SpreadDoesNotOverrideUserReservedNames(t *testing.T) {
	// If the previous step happens to contain keys literally named
	// `__root__` or `__state__`, they must survive the spread like any
	// other user data — they are not silently consumed by the runtime.
	in := map[string]any{
		rootVar:  "old-root",
		stateVar: "old-state",
		"a":      1,
	}

	got := runProjection(t, "..., b: 2", in)

	if got[rootVar] != "old-root" {
		t.Fatalf("user %s lost: got %v", rootVar, got[rootVar])
	}
	if got[stateVar] != "old-state" {
		t.Fatalf("user %s lost: got %v", stateVar, got[stateVar])
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestProjection_SpreadChainedTwice(t *testing.T) {
	// Simulates a bounded loop: two consecutive iterations bump
	// `attempt` while preserving the rest of the state.
	first := runProjection(t, "..., attempt: attempt + 1", map[string]any{
		"attempt": 1,
		"goal":    "keep me",
	})

	second := runProjection(t, "..., attempt: attempt + 1", first)

	if second["attempt"] != 3 {
		t.Fatalf("attempt = %v, want 3", second["attempt"])
	}
	if second["goal"] != "keep me" {
		t.Fatalf("goal lost after chained spread: %+v", second)
	}
}

// -----------------------------------------------------------------------------
// Preprocessor unit tests
// -----------------------------------------------------------------------------

func TestPreprocessSpread_Detection(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantSpread  bool
		wantContain string
		wantErr     bool
	}{
		{
			name:        "plain body",
			in:          "a: 1",
			wantSpread:  false,
			wantContain: "{ a: 1 }",
		},
		{
			name:        "bare spread",
			in:          "...",
			wantSpread:  true,
			wantContain: stateVar,
		},
		{
			name:        "leading spread with comma",
			in:          "..., a: 1",
			wantSpread:  true,
			wantContain: spreadMergeName,
		},
		{
			name:        "leading spread without space",
			in:          "...,a: 1",
			wantSpread:  true,
			wantContain: spreadMergeName,
		},
		{
			name:        "braced spread",
			in:          "{ ..., a: 1 }",
			wantSpread:  true,
			wantContain: spreadMergeName,
		},
		{
			name:        "misplaced spread",
			in:          "a: 1, ...",
			wantSpread:  false,
			wantContain: "",
			wantErr:     true,
		},
		{
			name:        "typo not detected as spread",
			in:          "..foo",
			wantSpread:  false,
			wantContain: "..foo",
		},
		{
			name:        "spread-like text inside string is ignored",
			in:          `msg: "waiting..."`,
			wantSpread:  false,
			wantContain: "waiting...",
		},
		{
			name:        "misplaced spread-like text in string is ignored",
			in:          `text: "...", count: 1`,
			wantSpread:  false,
			wantContain: `text: "...", count: 1`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, hasSpread, err := PreprocessSpread(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (out=%q)", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hasSpread != tc.wantSpread {
				t.Fatalf("hasSpread = %v, want %v", hasSpread, tc.wantSpread)
			}
			if !strings.Contains(out, tc.wantContain) {
				t.Fatalf("out = %q, want to contain %q", out, tc.wantContain)
			}
		})
	}
}

func TestTrimSpreadPrefix(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"...", "", true},
		{"..., a: 1", "a: 1", true},
		{"...,a: 1", "a: 1", true},
		{"...,    a: 1", "a: 1", true},
		{"...foo", "...foo", false},
		{"..foo", "..foo", false},
		{"a: 1", "a: 1", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := trimSpreadPrefix(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
