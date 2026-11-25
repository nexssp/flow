package compiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type expressionProjectionNested struct {
	Label string `json:"label"`
}

type expressionProjectionCustom struct {
	Value int `json:"value"`
}

func (value expressionProjectionCustom) MarshalJSON() ([]byte, error) {
	return json.Marshal("custom codec")
}

type expressionProjectionNamedString string

func (value expressionProjectionNamedString) MarshalJSON() ([]byte, error) {
	return json.Marshal("different wire representation")
}

type expressionProjectionPayload struct {
	Count    int                                   `json:"count"`
	Nested   expressionProjectionNested            `json:"nested"`
	Optional string                                `json:"optional,omitempty"`
	Custom   expressionProjectionCustom            `json:"custom"`
	Name     expressionProjectionNamedString       `json:"name"`
	Items    []expressionProjectionNested          `json:"items"`
	ByName   map[string]expressionProjectionCustom `json:"by_name"`
}

func TestExpressionProjectorPreservesDeclaredValuesAndJSONFieldNames(t *testing.T) {
	projector, err := NewExpressionProjector(reflect.TypeFor[expressionProjectionPayload]())
	if err != nil {
		t.Fatalf("NewExpressionProjector: %v", err)
	}
	payload := expressionProjectionPayload{
		Count:  7,
		Nested: expressionProjectionNested{Label: "ready"},
		Custom: expressionProjectionCustom{Value: 9},
		Name:   expressionProjectionNamedString("go-value"),
		Items:  []expressionProjectionNested{{Label: "item"}},
		ByName: map[string]expressionProjectionCustom{"entry": {Value: 11}},
	}
	binding, err := projector.Project(payload)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got, ok := binding["count"].(int); !ok || got != 7 {
		t.Fatalf("count = %#v (%T), want int(7)", binding["count"], binding["count"])
	}
	if got, ok := binding["nested"].(map[string]any); !ok || got["label"] != "ready" {
		t.Fatalf("nested = %#v, want JSON-tagged label map", binding["nested"])
	}
	if got, ok := binding["optional"].(string); !ok || got != "" {
		t.Fatalf("optional = %#v (%T), want present zero string", binding["optional"], binding["optional"])
	}
	custom, ok := binding["custom"].(map[string]any)
	if !ok || custom["value"] != 9 {
		t.Fatalf("custom = %#v, want declared field value despite custom marshaler", binding["custom"])
	}
	if got, nameOK := binding["name"].(expressionProjectionNamedString); !nameOK || got != "go-value" {
		t.Fatalf("name = %#v (%T), want original named Go string", binding["name"], binding["name"])
	}
	items, ok := binding["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want projected nested item", binding["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["label"] != "item" {
		t.Fatalf("item = %#v, want projected nested item", items[0])
	}
	byName, ok := binding["by_name"].(map[string]any)
	if !ok {
		t.Fatalf("by_name = %#v, want projected map value", binding["by_name"])
	}
	byNameEntry, ok := byName["entry"].(map[string]any)
	if !ok || byNameEntry["value"] != 11 {
		t.Fatalf("by_name entry = %#v, want projected map value", byName["entry"])
	}
}

type unexportedEmbedded struct {
	Name string
}

type inaccessibleProjectionPayload struct {
	unexportedEmbedded
}

func TestNewExpressionProjectorRejectsInaccessibleEmbeddedFields(t *testing.T) {
	_, err := NewExpressionProjector(reflect.TypeFor[inaccessibleProjectionPayload]())
	if err == nil || !strings.Contains(err.Error(), "unexported embedded field") {
		t.Fatalf("error = %v, want inaccessible embedded field diagnostic", err)
	}
}

type ExpressionProjectionEmbedded struct {
	Detail string `json:"detail"`
}

type flattenedProjectionPayload struct {
	ExpressionProjectionEmbedded
}

func TestExpressionProjectorFlattensExportedEmbeddedFields(t *testing.T) {
	projector, err := NewExpressionProjector(reflect.TypeFor[flattenedProjectionPayload]())
	if err != nil {
		t.Fatal(err)
	}
	//nolint:modernize // Go requires an embedded field's type in a struct literal.
	payload := flattenedProjectionPayload{}
	payload.Detail = "flattened"
	binding, err := projector.Project(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := binding["detail"]; got != "flattened" {
		t.Fatalf("detail = %#v, want flattened embedded field", got)
	}
}

type unsupportedProjectionPayload struct {
	Timestamp string `json:"timestamp,format:date"`
}

func TestNewExpressionProjectorRejectsUnsupportedJSONFieldOptions(t *testing.T) {
	_, err := NewExpressionProjector(reflect.TypeFor[unsupportedProjectionPayload]())
	if err == nil || !strings.Contains(err.Error(), "format:date") {
		t.Fatalf("error = %v, want unsupported format option diagnostic", err)
	}
}

type cyclicProjectionPayload struct {
	Next *cyclicProjectionPayload `json:"next"`
}

func TestExpressionProjectorBoundsRecursiveValues(t *testing.T) {
	projector, err := NewExpressionProjector(reflect.TypeFor[cyclicProjectionPayload]())
	if err != nil {
		t.Fatal(err)
	}
	payload := &cyclicProjectionPayload{}
	payload.Next = payload
	if _, err := projector.Project(payload); err == nil || !strings.Contains(err.Error(), "maximum projection depth") {
		t.Fatalf("error = %v, want bounded-depth error", err)
	}
}

type expressionProjectionMapKey string

type pointerProjectionPayload struct {
	Maybe *expressionProjectionNested                               `json:"maybe"`
	Value *int                                                      `json:"value"`
	Names map[expressionProjectionMapKey]expressionProjectionNested `json:"names"`
}

func TestExpressionProjectorHandlesNilPointersAndStringLikeMapKeys(t *testing.T) {
	projector, err := NewExpressionProjector(reflect.TypeFor[pointerProjectionPayload]())
	if err != nil {
		t.Fatal(err)
	}
	payload := pointerProjectionPayload{Names: map[expressionProjectionMapKey]expressionProjectionNested{
		"primary": {Label: "named key"},
	}}
	binding, err := projector.Project(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got, exists := binding["maybe"]; !exists || got != nil {
		t.Fatalf("maybe = %#v, want explicit null", got)
	}
	if got, exists := binding["value"]; !exists || got != nil {
		t.Fatalf("value = %#v, want explicit null", got)
	}
	names, ok := binding["names"].(map[string]any)
	if !ok {
		t.Fatalf("names = %#v, want string-keyed expression object", binding["names"])
	}
	primary, ok := names["primary"].(map[string]any)
	if !ok || primary["label"] != "named key" {
		t.Fatalf("names.primary = %#v, want projected nested value", names["primary"])
	}
}

type unsupportedMapKeyPayload struct {
	Values map[int]string `json:"values"`
}

func TestNewExpressionProjectorRejectsNonStringMapKeys(t *testing.T) {
	_, err := NewExpressionProjector(reflect.TypeFor[unsupportedMapKeyPayload]())
	if err == nil || !strings.Contains(err.Error(), "map key type int") {
		t.Fatalf("error = %v, want non-string map key diagnostic", err)
	}
}

type dynamicProjectionPayload struct {
	Value any `json:"value"`
}

type unsupportedProjectionKindPayload struct {
	Value chan int `json:"value"`
}

func TestNewExpressionProjectorRejectsDynamicAndUnsupportedFieldTypes(t *testing.T) {
	for _, test := range []struct {
		name      string
		payload   reflect.Type
		wantError string
	}{
		{
			name:      "interface",
			payload:   reflect.TypeFor[dynamicProjectionPayload](),
			wantError: "not a statically typed",
		},
		{
			name:      "channel",
			payload:   reflect.TypeFor[unsupportedProjectionKindPayload](),
			wantError: "not supported",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewExpressionProjector(test.payload)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}
