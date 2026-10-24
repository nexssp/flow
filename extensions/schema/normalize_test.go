package schema

import (
	"testing"
)

type NormalizeEmbeddedFields struct {
	EmbeddedName string `json:"embedded_name"`
	Ignored      string `json:"-"`
}

type normalizeNestedFields struct {
	Count    int     `json:"count"`
	Optional *string `json:"optional"`
}

type normalizeObjectFixture struct {
	NormalizeEmbeddedFields
	DisplayName string                `json:"display_name"`
	Untaged     string                `json:""`
	Empty       string                `json:"empty,omitempty"`
	Nested      normalizeNestedFields `json:"nested"`
	Pointer     *string               `json:"pointer"`
	Ignored     string                `json:"-"`
	unexported  string
}

func TestNormalizeObjectUsesJSONFieldSemantics(t *testing.T) {
	t.Parallel()

	pointer := "present"
	got, err := NormalizeObject(&normalizeObjectFixture{
		NormalizeEmbeddedFields: NormalizeEmbeddedFields{EmbeddedName: "from-embed", Ignored: "hidden"},
		DisplayName:             "Ada",
		Untaged:                 "uses-default-name",
		Nested:                  normalizeNestedFields{Count: 3},
		Pointer:                 &pointer,
		Ignored:                 "hidden",
		unexported:              "hidden",
	})
	if err != nil {
		t.Fatalf("NormalizeObject: %v", err)
	}

	for key, want := range map[string]any{
		"embedded_name": "from-embed",
		"display_name":  "Ada",
		"Untaged":       "uses-default-name",
		"pointer":       "present",
	} {
		if got[key] != want {
			t.Errorf("%q = %#v, want %#v", key, got[key], want)
		}
	}
	for _, omitted := range []string{"empty", "Ignored", "unexported"} {
		if _, exists := got[omitted]; exists {
			t.Errorf("unexpected JSON field %q in %#v", omitted, got)
		}
	}

	nested, ok := got["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested = %T, want map[string]any", got["nested"])
	}
	if nested["count"] != float64(3) {
		t.Errorf("nested count = %#v, want float64(3)", nested["count"])
	}
	if value, exists := nested["optional"]; !exists || value != nil {
		t.Errorf("nil nested pointer = %#v (present=%t), want JSON null", value, exists)
	}
}

func TestNormalizeObjectAcceptsNonNilPointerAndEmptyObject(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{
		"pointer":      &normalizeObjectFixture{DisplayName: "Ada"},
		"empty object": map[string]any{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeObject(value)
			if err != nil {
				t.Fatalf("NormalizeObject(%T): %v", value, err)
			}
			if got == nil {
				t.Fatal("NormalizeObject returned a nil map for an object")
			}
		})
	}
}

func TestNormalizeObjectRejectsNilAndNonObjects(t *testing.T) {
	t.Parallel()

	var nilPointer *normalizeObjectFixture
	var nilMap map[string]any
	for name, value := range map[string]any{
		"nil interface": nil,
		"nil pointer":   nilPointer,
		"nil map":       nilMap,
		"scalar":        "not an object",
		"array":         []int{1, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeObject(value)
			if err == nil {
				t.Fatalf("NormalizeObject(%T) = %#v, want error", value, got)
			}
			if got != nil {
				t.Fatalf("NormalizeObject(%T) = %#v, want nil map on error", value, got)
			}
		})
	}
}
