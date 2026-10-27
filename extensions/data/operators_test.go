package data

import (
	"fmt"
	"iter"
	"reflect"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestWhereFiltersRowsByExpression(t *testing.T) {
	t.Parallel()
	rows := sequence(
		map[string]any{"id": "a", "status": "paid"},
		map[string]any{"id": "b", "status": "pending"},
		map[string]any{"id": "c", "status": "paid"},
	)
	got, err := collect(Where(whereConfig{Expression: `status == "paid"`})(rows))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(got), 2)
	first, ok := got[0].(map[string]any)
	ktest.RequireCondition(t, ok, "first row has type %T", got[0])
	second, ok := got[1].(map[string]any)
	ktest.RequireCondition(t, ok, "second row has type %T", got[1])
	ktest.RequireEqual(t, first["id"], "a")
	ktest.RequireEqual(t, second["id"], "c")
}

func TestWhereSupportsRowAndItemAliases(t *testing.T) {
	t.Parallel()
	got, err := collect(Where(whereConfig{Expression: `row.id == "a" && item.id == "a"`})(sequence(map[string]any{"id": "a"})))
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(got), 1)
}

func TestWhereReportsInputAndExpressionErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		expression string
		input      any
		want       string
	}{
		{name: "empty expression", expression: " ", input: map[string]any{}, want: "expression is required"},
		{name: "invalid expression", expression: "age ==", input: map[string]any{}, want: "invalid expression"},
		{name: "non-boolean result", expression: "age + 1", input: map[string]any{"age": 2}, want: "must return a boolean"},
		{name: "runtime error", expression: "age > 1", input: map[string]any{"age": "old"}, want: "expression failed"},
		{name: "non-object input", expression: "true", input: 42, want: "input row is not a JSON object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := collect(Where(whereConfig{Expression: test.expression})(sequence(test.input)))
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestSelectKeepsRequestedFieldsAndMissingValues(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "x", "label": "refund", "discard": true}
	got, err := collect(Select(selectConfig{Fields: "id, label, absent"})(sequence(row)))
	ktest.RequireNoError(t, err)
	selected, ok := got[0].(map[string]any)
	ktest.RequireCondition(t, ok, "selected row has type %T", got[0])
	ktest.RequireEqual(t, len(selected), 3)
	ktest.RequireEqual(t, selected["id"], "x")
	ktest.RequireEqual(t, selected["label"], "refund")
	ktest.RequireEqual(t, selected["absent"], nil)
}

func TestSelectWildcardCopiesEveryField(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "x", "label": "refund"}
	got, err := collect(Select(selectConfig{Fields: "*"})(sequence(row)))
	ktest.RequireNoError(t, err)
	selected, ok := got[0].(map[string]any)
	ktest.RequireCondition(t, ok, "selected row has type %T", got[0])
	ktest.RequireEqual(t, selected, row)
	selected["added"] = true
	ktest.RequireCondition(t, row["added"] == nil, "wildcard output aliases the input map")
}

func TestSelectReportsInvalidFieldsAndRows(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		field string
		input any
		want  string
	}{
		{name: "empty fields", field: " , ", input: map[string]any{}, want: "fields is required"},
		{name: "wildcard with field", field: "*,a", input: map[string]any{"a": 1}, want: "cannot be combined"},
		{name: "non-object input", field: "id", input: "not an object", want: "input row is not a JSON object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := collect(Select(selectConfig{Fields: test.field})(sequence(test.input)))
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestSplitUsesStableKeyAndDistributesRowsApproximately(t *testing.T) {
	t.Parallel()
	rows := make([]any, 10_000)
	for index := range rows {
		rows[index] = map[string]any{"id": fmt.Sprintf("case-%05d", index)}
	}

	membership := make(map[string]string, len(rows))
	counts := make(map[string]int)
	for _, partition := range []string{"train", "validation", "test"} {
		cfg := splitConfig{Partition: partition, Train: 80, Validation: 10, Test: 10, Seed: 42, Key: "id"}
		got, err := collect(Split(cfg)(sequence(rows...)))
		ktest.RequireNoError(t, err)
		for _, value := range got {
			row, ok := value.(map[string]any)
			ktest.RequireCondition(t, ok, "partition row has type %T", value)
			id, ok := row["id"].(string)
			ktest.RequireCondition(t, ok, "row id has type %T", row["id"])
			ktest.RequireCondition(t, membership[id] == "", "id %q appears in multiple partitions", id)
			membership[id] = partition
			counts[partition]++
		}
	}
	ktest.RequireEqual(t, len(membership), len(rows))
	ktest.RequireCondition(t, counts["train"] >= 7800 && counts["train"] <= 8200, "train count = %d, want 7800..8200", counts["train"])
	ktest.RequireCondition(t, counts["validation"] >= 800 && counts["validation"] <= 1200, "validation count = %d, want 800..1200", counts["validation"])
	ktest.RequireCondition(t, counts["test"] >= 800 && counts["test"] <= 1200, "test count = %d, want 800..1200", counts["test"])

	reversed := append([]any(nil), rows...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	reorderedMembership := make(map[string]string, len(rows))
	for _, partition := range []string{"train", "validation", "test"} {
		cfg := splitConfig{Partition: partition, Train: 80, Validation: 10, Test: 10, Seed: 42, Key: "id"}
		got, err := collect(Split(cfg)(sequence(reversed...)))
		ktest.RequireNoError(t, err)
		for _, value := range got {
			row, ok := value.(map[string]any)
			ktest.RequireCondition(t, ok, "partition row has type %T", value)
			id, ok := row["id"].(string)
			ktest.RequireCondition(t, ok, "row id has type %T", row["id"])
			reorderedMembership[id] = partition
		}
	}
	ktest.RequireCondition(t, reflect.DeepEqual(membership, reorderedMembership), "partition membership changed when input order changed")
}

func TestSplitAssignsEqualKeysToTheSamePartition(t *testing.T) {
	t.Parallel()
	rows := sequence(
		map[string]any{"id": "shared", "copy": 1},
		map[string]any{"id": "shared", "copy": 2},
	)
	selectedPartition := ""
	for _, partition := range []string{"train", "validation", "test"} {
		cfg := splitConfig{Partition: partition, Train: 60, Validation: 20, Test: 20, Seed: 42, Key: "id"}
		got, err := collect(Split(cfg)(rows))
		ktest.RequireNoError(t, err)
		if len(got) == 0 {
			continue
		}
		ktest.RequireEqual(t, len(got), 2)
		ktest.RequireCondition(t, selectedPartition == "", "equal keys selected into multiple partitions")
		selectedPartition = partition
	}
	ktest.RequireCondition(t, selectedPartition != "", "equal key was not assigned to any partition")
}

func TestSplitSupportsBoundaryPercentages(t *testing.T) {
	t.Parallel()
	rows := sequence(
		map[string]any{"id": "a"},
		map[string]any{"id": "b"},
		map[string]any{"id": "c"},
	)
	for _, test := range []struct {
		name       string
		train      int
		validation int
		test       int
		selected   string
	}{
		{name: "all train", train: 100, selected: "train"},
		{name: "all validation", validation: 100, selected: "validation"},
		{name: "all test", test: 100, selected: "test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, partition := range []string{"train", "validation", "test"} {
				cfg := splitConfig{Partition: partition, Train: test.train, Validation: test.validation, Test: test.test, Seed: 42}
				got, err := collect(Split(cfg)(rows))
				ktest.RequireNoError(t, err)
				want := 0
				if partition == test.selected {
					want = 3
				}
				ktest.RequireEqual(t, len(got), want)
			}
		})
	}
}

func TestSplitRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		cfg  splitConfig
		want string
	}{
		{name: "unknown partition", cfg: splitConfig{Partition: "other", Train: 100}, want: "partition must be"},
		{name: "negative percentage", cfg: splitConfig{Partition: "train", Train: -1, Validation: 1, Test: 100}, want: "between 0 and 100"},
		{name: "percentage over 100", cfg: splitConfig{Partition: "train", Train: 101, Validation: 0, Test: -1}, want: "between 0 and 100"},
		{name: "percentages do not sum", cfg: splitConfig{Partition: "train", Train: 70, Validation: 10, Test: 10}, want: "sum to 100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := collect(Split(test.cfg)(sequence()))
			ktest.RequireErrorContains(t, err, test.want)
		})
	}
}

func TestSplitRejectsMissingOrNullStableKey(t *testing.T) {
	t.Parallel()
	cfg := splitConfig{Partition: "train", Train: 80, Validation: 10, Test: 10, Seed: 1}
	for _, row := range []any{
		map[string]any{"name": "row"},
		map[string]any{"id": nil},
	} {
		_, err := collect(Split(cfg)(sequence(row)))
		ktest.RequireErrorContains(t, err, "stable key field id is missing or null")
	}
}

func TestSplitRejectsNonObjectInput(t *testing.T) {
	t.Parallel()
	cfg := splitConfig{Partition: "train", Train: 100}
	_, err := collect(Split(cfg)(sequence("not an object")))
	ktest.RequireErrorContains(t, err, "input row is not a JSON object")
}

func sequence(values ...any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for _, value := range values {
			if !yield(value, nil) {
				return
			}
		}
	}
}

func collect(source iter.Seq2[any, error]) ([]any, error) {
	var values []any
	var firstErr error
	for value, err := range source {
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		values = append(values, value)
	}
	return values, firstErr
}
