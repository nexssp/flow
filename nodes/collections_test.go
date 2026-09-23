package nodes

import (
	"context"
	"testing"
)

// The purpose of this file is to lock the contract for using expr-lang
// built-in array functions inside projections. These functions are not
// provided by the runtime — they come from expr-lang itself — but the
// DSL layer (PreprocessDotNotation, PreprocessSpread) must not corrupt
// the expressions that call them.
//
// Conventions used in these tests:
//
//   - `#` refers to the current element inside a predicate.
//   - `#.field` accesses a field of the current element.
//   - `|` pipes the left-hand value into the right-hand call.
//
// If any of these tests fails after a dependency upgrade, the fix is
// almost always in PreprocessDotNotation, not in the projection
// runtime.

// eval runs a projection body against input and returns the result as
// a slice, so tests can assert on the shape produced by array
// functions. It fails the test on any error.
func evalSlice(t *testing.T, body string, input any) []any {
	t.Helper()

	act, err := NewProjectionAction(body)
	if err != nil {
		t.Fatalf("compile %q: %v", body, err)
	}

	out, err := act.Do(context.Background(), input)
	if err != nil {
		t.Fatalf("exec %q: %v", body, err)
	}

	slice, ok := out.([]any)
	if !ok {
		t.Fatalf("exec %q: expected []any, got %T", body, out)
	}

	return slice
}

// findingsFixture is the canonical test input for array operations.
// It is intentionally small but covers duplicated severities, missing
// fields, and mixed categories.
func findingsFixture() map[string]any {
	return map[string]any{
		"findings": []any{
			map[string]any{"file": "a.go", "line": 10, "severity": "warning", "category": "style", "message": "m1"},
			map[string]any{"file": "b.go", "line": 20, "severity": "critical", "category": "bug", "message": "m2"},
			map[string]any{"file": "c.go", "line": 30, "severity": "error", "category": "security", "message": "m3"},
			map[string]any{"file": "d.go", "line": 40, "severity": "critical", "category": "bug", "message": "m4"},
			map[string]any{"file": "e.go", "line": 50, "severity": "warning", "category": "style", "message": "m5"},
		},
	}
}

// -----------------------------------------------------------------------------
// sortBy
// -----------------------------------------------------------------------------

func TestCollections_SortBySeverity(t *testing.T) {
	got := evalSlice(t, `sortBy(findings, #.severity)`, findingsFixture())

	if len(got) != 5 {
		t.Fatalf("expected 5 items, got %d", len(got))
	}

	// Alphabetical order by severity string:
	// critical, critical, error, warning, warning
	wantOrder := []string{"critical", "critical", "error", "warning", "warning"}

	for i, want := range wantOrder {
		item, _ := got[i].(map[string]any)
		if item["severity"] != want {
			t.Fatalf("index %d: severity = %v, want %v", i, item["severity"], want)
		}
	}
}

func TestCollections_SortByDescending(t *testing.T) {
	got := evalSlice(t, `sortBy(findings, #.line, "desc")`, findingsFixture())

	if len(got) != 5 {
		t.Fatalf("expected 5 items, got %d", len(got))
	}

	// Lines are 10, 20, 30, 40, 50; descending is 50, 40, 30, 20, 10.
	wantLines := []int{50, 40, 30, 20, 10}

	for i, want := range wantLines {
		item, _ := got[i].(map[string]any)
		if item["line"] != want {
			t.Fatalf("index %d: line = %v, want %v", i, item["line"], want)
		}
	}
}

// -----------------------------------------------------------------------------
// groupBy
// -----------------------------------------------------------------------------

func TestCollections_GroupBySeverity(t *testing.T) {
	act, err := NewProjectionAction(`groupBy(findings, #.severity)`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := act.Do(context.Background(), findingsFixture())
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	groups, ok := out.(map[any][]any)
	if !ok {
		t.Fatalf("expected map[any][]any, got %T", out)
	}

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d (%v)", len(groups), groups)
	}

	for _, key := range []string{"critical", "error", "warning"} {
		if _, ok := groups[key]; !ok {
			t.Fatalf("missing group %q in %v", key, groups)
		}
	}
}

// -----------------------------------------------------------------------------
// filter / count / first / last / take
// -----------------------------------------------------------------------------

func TestCollections_FilterCritical(t *testing.T) {
	got := evalSlice(t, `filter(findings, #.severity == "critical")`, findingsFixture())

	if len(got) != 2 {
		t.Fatalf("expected 2 critical, got %d", len(got))
	}
}

func TestCollections_CountWithPredicate(t *testing.T) {
	act, err := NewProjectionAction(`count(findings, #.severity == "critical")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := act.Do(context.Background(), findingsFixture())
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if out != 2 {
		t.Fatalf("count = %v (%T), want 2", out, out)
	}
}

func TestCollections_FirstAndLast(t *testing.T) {
	act, err := NewProjectionAction(`first(findings)`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := act.Do(context.Background(), findingsFixture())
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	item, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("first returned %T, want map[string]any", out)
	}
	if item["file"] != "a.go" {
		t.Fatalf("first file = %v, want a.go", item["file"])
	}
}

func TestCollections_Take(t *testing.T) {
	got := evalSlice(t, `take(findings, 2)`, findingsFixture())

	if len(got) != 2 {
		t.Fatalf("take(2) returned %d items", len(got))
	}
}

// -----------------------------------------------------------------------------
// Pipe operator
// -----------------------------------------------------------------------------

func TestCollections_PipeChain(t *testing.T) {
	// The pipe operator is native to expr-lang. This test locks that
	// the DSL does not interfere with it.
	body := `findings | filter(#.severity == "critical") | take(1)`
	got := evalSlice(t, body, findingsFixture())

	if len(got) != 1 {
		t.Fatalf("expected 1 item after pipe chain, got %d", len(got))
	}

	item, _ := got[0].(map[string]any)
	if item["severity"] != "critical" {
		t.Fatalf("severity = %v, want critical", item["severity"])
	}
}

func TestCollections_PipeWithSortAndTake(t *testing.T) {
	// A more realistic pipeline: filter → sort → take.
	body := `findings
		| filter(#.severity != "warning")
		| sortBy(#.line, "desc")
		| take(2)`

	got := evalSlice(t, body, findingsFixture())

	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}

	// After filter: line 20 (critical), 30 (error), 40 (critical).
	// After sort desc: 40, 30, 20.
	// After take(2): 40, 30.
	wantLines := []int{40, 30}

	for i, want := range wantLines {
		item, _ := got[i].(map[string]any)
		if item["line"] != want {
			t.Fatalf("index %d: line = %v, want %v", i, item["line"], want)
		}
	}
}

// -----------------------------------------------------------------------------
// uniq / flatten / concat
// -----------------------------------------------------------------------------

func TestCollections_Uniq(t *testing.T) {
	input := map[string]any{
		"tags": []any{"a", "b", "a", "c", "b"},
	}

	got := evalSlice(t, `uniq(tags)`, input)
	if len(got) != 3 {
		t.Fatalf("uniq returned %d items, want 3 (%v)", len(got), got)
	}
}

func TestCollections_Flatten(t *testing.T) {
	input := map[string]any{
		"matrix": []any{
			[]any{1, 2},
			[]any{3, 4},
		},
	}

	got := evalSlice(t, `flatten(matrix)`, input)
	if len(got) != 4 {
		t.Fatalf("flatten returned %d items, want 4 (%v)", len(got), got)
	}
}

func TestCollections_Concat(t *testing.T) {
	input := map[string]any{
		"a": []any{1, 2},
		"b": []any{3, 4},
	}

	got := evalSlice(t, `concat(a, b)`, input)
	if len(got) != 4 {
		t.Fatalf("concat returned %d items, want 4 (%v)", len(got), got)
	}
}

// -----------------------------------------------------------------------------
// Spread + array functions together
// -----------------------------------------------------------------------------

func TestCollections_SpreadPreservesSortedFindings(t *testing.T) {
	input := findingsFixture()
	input["attempt"] = 1

	act, err := NewProjectionAction(
		`..., sorted: sortBy(findings, #.severity), attempt: attempt + 1`,
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := act.Do(context.Background(), input)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}

	if result["attempt"] != 2 {
		t.Fatalf("attempt = %v, want 2", result["attempt"])
	}

	sorted, ok := result["sorted"].([]any)
	if !ok {
		t.Fatalf("sorted field missing or wrong type: %T", result["sorted"])
	}

	if len(sorted) != 5 {
		t.Fatalf("sorted has %d items, want 5", len(sorted))
	}

	// Original findings field must still be present (spread preserved it).
	if _, ok := result["findings"]; !ok {
		t.Fatalf("spread dropped `findings`: %v", result)
	}
}

// -----------------------------------------------------------------------------
// PreprocessDotNotation contract — `#.field` survives untouched
// -----------------------------------------------------------------------------

func TestPreprocessDotNotation_PreservesPredicateScope(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "hash dot field survives",
			in:   `sortBy(users, #.Age)`,
			want: `sortBy(users, #.Age)`,
		},
		{
			name: "hash dot nested field survives",
			in:   `groupBy(items, #.meta.kind)`,
			want: `groupBy(items, #.meta.kind)`,
		},
		{
			name: "leading dot still flattens",
			in:   `.severity`,
			want: `severity`,
		},
		{
			name: "plain member access untouched",
			in:   `user.name`,
			want: `user.name`,
		},
		{
			name: "index then member untouched",
			in:   `arr[0].name`,
			want: `arr[0].name`,
		},
		{
			name: "function call then member untouched",
			in:   `first(users).Age`,
			want: `first(users).Age`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := PreprocessDotNotation(tc.in)
			if got != tc.want {
				t.Fatalf("PreprocessDotNotation(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}
