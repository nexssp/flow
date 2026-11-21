package loop

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
)

func TestEachItemsListKinds(t *testing.T) {
	var nilAnySlice []any
	var nilIntSlice []int
	for _, tc := range []struct {
		name  string
		input any
		want  []any
	}{
		{name: "typed nil any slice", input: nilAnySlice, want: []any{}},
		{name: "typed nil integer slice", input: nilIntSlice, want: []any{}},
		{name: "integer slice", input: []int{2, 4}, want: []any{2, 4}},
		{name: "array", input: [2]string{"a", "b"}, want: []any{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eachItems(tc.input)
			if err != nil {
				t.Fatalf("eachItems: %v", err)
			}
			if got == nil || len(got) != len(tc.want) {
				t.Fatalf("eachItems = %#v, want non-nil list of length %d", got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("eachItems[%d] = %#v, want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestEachItemsRejectsNilAndNilPointer(t *testing.T) {
	var nilSlicePointer *[]int
	for _, tc := range []struct {
		name  string
		input any
	}{
		{name: "untyped nil", input: nil},
		{name: "nil pointer to slice", input: nilSlicePointer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eachItems(tc.input)
			if got := xerr.KindFrom(err); got != xerr.KindValidation {
				t.Fatalf("eachItems error kind = %s, want %s: %v", got, xerr.KindValidation, err)
			}
		})
	}
}

func TestEachItemsRejectsOversizedTypedSliceBeforeConversion(t *testing.T) {
	_, err := eachItems(make([]int, 100_001))
	if got := xerr.KindFrom(err); got != xerr.KindBadRequest {
		t.Fatalf("eachItems error kind = %s, want %s: %v", got, xerr.KindBadRequest, err)
	}
	if err == nil || !strings.Contains(err.Error(), "100001 items exceeds limit of 100000") {
		t.Fatalf("eachItems error = %v, want fanout item-count limit", err)
	}
}
