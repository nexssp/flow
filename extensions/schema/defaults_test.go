package schema

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyDefaults(t *testing.T) {
	t.Parallel()

	s := Schema{
		Name: "Settings",
		Fields: []Field{
			{Name: "Name", JSONName: "name", Type: "string", Kind: KindString, Tags: map[string]string{"default": "standard"}},
			{Name: "Depth", JSONName: "depth", Type: "int", Kind: KindInt, Tags: map[string]string{"default": "2"}},
			{Name: "Enabled", JSONName: "enabled", Type: "bool", Kind: KindBool, Tags: map[string]string{"default": "true"}},
			{Name: "Debug", JSONName: "debug", Type: "bool", Kind: KindBool, Tags: map[string]string{"default": "false"}},
			{Name: "Ratio", JSONName: "ratio", Type: "float64", Kind: KindFloat, Tags: map[string]string{"default": "1.5"}},
			{Name: "Labels", JSONName: "labels", Type: "[]string", Kind: KindString, Slice: true, Tags: map[string]string{"default": `["a","b"]`}},
			{Name: "Options", JSONName: "options", Type: "map[string]any", Kind: KindAny, Map: true, Tags: map[string]string{"default": `{"mode":"fast"}`}},
		},
	}
	input := map[string]any{"name": "custom", "enabled": nil, "extra": 42}

	got, err := ApplyDefaults(s, input)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "custom" {
		t.Errorf("explicit name = %#v, want custom", got["name"])
	}
	if value, ok := got["enabled"]; !ok || value != nil {
		t.Errorf("explicit nil enabled = %#v, present=%t; want nil and present", value, ok)
	}
	if got["depth"] != 2 || got["enabled"] != nil || got["debug"] != false || got["ratio"] != 1.5 {
		t.Errorf("scalar defaults = %#v", got)
	}
	if !reflect.DeepEqual(got["labels"], []any{"a", "b"}) {
		t.Errorf("labels default = %#v, want []any{\"a\", \"b\"}", got["labels"])
	}
	if !reflect.DeepEqual(got["options"], map[string]any{"mode": "fast"}) {
		t.Errorf("options default = %#v, want map[mode:fast]", got["options"])
	}
	if got["extra"] != 42 {
		t.Errorf("unrecognized input field was lost: %#v", got)
	}
	if len(input) != 3 {
		t.Errorf("ApplyDefaults mutated input: %#v", input)
	}
}

func TestApplyDefaultsAcceptsJSONAndStringDefaultsForAny(t *testing.T) {
	t.Parallel()

	s := Schema{Name: "AnyValues", Fields: []Field{
		{Name: "Text", JSONName: "text", Type: "any", Kind: KindAny, Tags: map[string]string{"default": "plain text"}},
		{Name: "Enabled", JSONName: "enabled", Type: "any", Kind: KindAny, Tags: map[string]string{"default": "true"}},
	}}
	got, err := ApplyDefaults(s, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got["text"] != "plain text" || got["enabled"] != true {
		t.Fatalf("defaults = %#v, want plain text and true", got)
	}
}

func TestApplyDefaultsRejectsNonObject(t *testing.T) {
	t.Parallel()

	_, err := ApplyDefaults(Schema{Name: "Settings"}, "not an object")
	if err == nil || !strings.Contains(err.Error(), "expected object") {
		t.Fatalf("error = %v, want expected-object error", err)
	}
}

func TestValidateAppliesDefaultsBeforeRequiredCheck(t *testing.T) {
	t.Parallel()

	s := Schema{Name: "Settings", Fields: []Field{{
		Name: "Name", JSONName: "name", Type: "string", Kind: KindString,
		Tags: map[string]string{"default": "standard", "validate": "required"},
	}}}
	if err := Validate(t.Context(), s, map[string]any{}); err != nil {
		t.Fatalf("Validate() error = %v, want default to satisfy required", err)
	}
}

func TestApplyDefaultsRejectsInvalidProgrammaticDefault(t *testing.T) {
	t.Parallel()

	s := Schema{Name: "Settings", Fields: []Field{{
		Name: "Depth", JSONName: "depth", Type: "int", Kind: KindInt,
		Tags: map[string]string{"default": "many"},
	}}}
	_, err := ApplyDefaults(s, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), `field "depth" default`) {
		t.Fatalf("error = %v, want invalid depth default", err)
	}
}

func TestToJSONSchemaIncludesTypedDefault(t *testing.T) {
	t.Parallel()

	s := Schema{Name: "Settings", Fields: []Field{{
		Name: "Depth", JSONName: "depth", Type: "int", Kind: KindInt,
		Tags: map[string]string{"default": "2"},
	}}}
	got := ToJSONSchema(s)
	properties, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties have type %T, want map[string]any", got["properties"])
	}
	depth, ok := properties["depth"].(map[string]any)
	if !ok {
		t.Fatalf("depth has type %T, want map[string]any", properties["depth"])
	}
	if depth["default"] != 2 {
		t.Fatalf("JSON Schema default = %#v, want integer 2", depth["default"])
	}
}

func TestToJSONSchemaPreservesAnyAndStructArrayTypes(t *testing.T) {
	t.Parallel()

	s := Schema{Name: "Payload", Fields: []Field{
		{Name: "Value", JSONName: "value", Type: "any", Kind: KindAny},
		{Name: "Items", JSONName: "items", Type: "[]Item", Kind: KindStruct, Slice: true},
	}}
	properties, ok := ToJSONSchema(s)["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties must be an object")
	}
	value, ok := properties["value"].(map[string]any)
	if !ok {
		t.Fatal("value schema must be an object")
	}
	if _, constrained := value["type"]; constrained {
		t.Fatalf("any field schema = %#v, want no restrictive type", value)
	}
	array, ok := properties["items"].(map[string]any)
	if !ok {
		t.Fatal("array schema must be an object")
	}
	items, ok := array["items"].(map[string]any)
	if !ok {
		t.Fatal("array item schema must be an object")
	}
	if items["type"] != typeObject {
		t.Fatalf("struct array item schema = %#v, want object", items)
	}
}
