package schema

import (
	"errors"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func sampleSchema() Schema {
	return Schema{
		Name: "User",
		Fields: []Field{
			{Name: "Name", JSONName: "name", Kind: KindString, Tags: map[string]string{"validate": "required"}},
			{Name: "Age", JSONName: "age", Kind: KindInt},
			{Name: "Tags", JSONName: "tags", Kind: KindString, Slice: true},
			{Name: "Attrs", JSONName: "attrs", Kind: KindAny, Map: true},
			{Name: "Active", JSONName: "active", Kind: KindBool, Pointer: true},
		},
	}
}

// requireValidationField extracts the *xerr.AppError and asserts that
// it carries a detail for the given field. Returns the detail so the
// caller can inspect it further.
func requireValidationField(tb testing.TB, err error, field string) xerr.ValidationDetail {
	tb.Helper()
	appErr, ok := errors.AsType[*xerr.AppError](err)
	ktest.RequireCondition(tb, ok, "error is %T, want *xerr.AppError", err)
	ktest.RequireEqual(tb, appErr.Kind, xerr.KindValidation)

	for _, detail := range appErr.ValidationDetails {
		if detail.Field == field {
			return detail
		}
	}
	tb.Fatalf("no validation detail for field %q in %+v", field, appErr.ValidationDetails)
	return xerr.ValidationDetail{}
}

func TestValidate_Success(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"name":   "Maksymilian",
		"age":    42,
		"tags":   []any{"a"},
		"attrs":  map[string]any{"x": 1},
		"active": nil,
	}
	ktest.RequireNoError(t, Validate(sampleSchema(), payload))
}

func TestValidate_MissingRequired(t *testing.T) {
	t.Parallel()
	err := Validate(sampleSchema(), map[string]any{"age": 42})
	detail := requireValidationField(t, err, "name")
	ktest.RequireEqual(t, detail.Validation, "required")
}

func TestValidate_OptionalMissing(t *testing.T) {
	t.Parallel()
	ktest.RequireNoError(t, Validate(sampleSchema(), map[string]any{"name": "Maksymilian"}))
}

func TestValidate_TypeErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		payload      map[string]any
		field        string
		wantValidate string
	}{
		{"wrong string", map[string]any{"name": 42}, "name", "string"},
		{"wrong int", map[string]any{"name": "Maksymilian", "age": "young"}, "age", "number"},
		{"wrong slice", map[string]any{"name": "Maksymilian", "tags": "not-a-slice"}, "tags", "slice"},
		{"wrong map", map[string]any{"name": "Maksymilian", "attrs": "not-a-map"}, "attrs", "map"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(sampleSchema(), c.payload)
			detail := requireValidationField(t, err, c.field)
			ktest.RequireEqual(t, detail.Validation, c.wantValidate)
		})
	}
}

func TestValidate_NotAnObject(t *testing.T) {
	t.Parallel()
	err := Validate(sampleSchema(), "not-an-object")
	ktest.RequireErrorKind(t, err, xerr.KindValidation)
	ktest.RequireErrorContains(t, err, "expected object")
}

func TestValidate_MultipleFailures(t *testing.T) {
	t.Parallel()
	// Both `name` (missing required) and `age` (wrong type) fail in
	// one call; both details must be reported, not just the first.
	payload := map[string]any{"age": "young"}
	err := Validate(sampleSchema(), payload)

	appErr, ok := errors.AsType[*xerr.AppError](err)
	ktest.RequireCondition(t, ok, "error is %T, want *xerr.AppError", err)
	ktest.RequireEqual(t, len(appErr.ValidationDetails), 2)
}

func TestSchemaByName(t *testing.T) {
	t.Parallel()
	meta := map[string]any{"schemas": []Schema{{Name: "A"}, {Name: "B"}}}
	got, ok := ByName(meta, "B")
	ktest.RequireCondition(t, ok, "B not found")
	ktest.RequireEqual(t, got.Name, "B")

	_, ok = ByName(meta, "C")
	ktest.RequireCondition(t, !ok, "C should not be found")
}
