package schema

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler/secret"
)

func TestSecretSchemaFieldMaterializesOpaqueValue(t *testing.T) {
	field, err := parseSchemaField(`APIKey secret ` + "`json:\"api_key\" validate:\"required\"`")
	if err != nil {
		t.Fatalf("parse secret field: %v", err)
	}
	if field.Kind != KindSecret || field.JSONName != "api_key" {
		t.Fatalf("parsed field = %+v, want KindSecret api_key", field)
	}
	schema := Schema{Name: "Request", Fields: []Field{*field}}
	values, err := ApplyDefaultsAndValidate(context.Background(), schema, map[string]any{"api_key": "sensitive-canary"})
	if err != nil {
		t.Fatalf("apply defaults and validate: %v", err)
	}
	value, ok := values["api_key"].(secret.Value)
	if !ok || !secret.IsValue(value) {
		t.Fatalf("materialized value = %T, want opaque secret.Value", values["api_key"])
	}
	sanitized, ok := secret.Sanitize(values).(map[string]any)
	if !ok {
		t.Fatalf("sanitized payload type = %T, want map[string]any", secret.Sanitize(values))
	}
	if got := sanitized["api_key"]; got != secret.Redacted {
		t.Fatalf("sanitized schema payload field = %#v, want redaction marker", got)
	}
}

func TestSecretSchemaRejectsStringOutsideInputBoundary(t *testing.T) {
	schema := Schema{Name: "Request", Fields: []Field{{
		Name: "APIKey", JSONName: "api_key", Type: "secret", Kind: KindSecret,
	}}}
	if err := ValidateWithoutDefaults(context.Background(), schema, map[string]any{"api_key": "sensitive-canary"}); err == nil {
		t.Fatal("validate-only path accepted a plain string for secret")
	}
	if err := ValidateWithoutDefaults(context.Background(), schema, map[string]any{"api_key": secret.New("sensitive-canary")}); err != nil {
		t.Fatalf("validate-only path rejected an already typed secret: %v", err)
	}
}

func TestSecretSchemaForbidsDefaultsAndUnsafeValidation(t *testing.T) {
	for _, line := range []string{
		`APIKey secret ` + "`default:\"value\"`",
		`APIKey secret ` + "`validate:\"min=3\"`",
		`APIKey *secret`,
		`APIKey []secret`,
	} {
		field, err := parseSchemaField(line)
		if err != nil {
			t.Fatalf("parse %q: %v", line, err)
		}
		if err := validateSecretFieldDeclarations([]Field{*field}); err == nil {
			t.Errorf("secret declaration %q unexpectedly passed validation", line)
		}
	}
	field := Field{Name: "APIKey", JSONName: "api_key", Kind: KindSecret, HasTypedDefault: true, TypedDefault: secret.New("sensitive-canary")}
	if err := validateSchemaDefaults([]Field{field}); err == nil {
		t.Fatal("typed programmatic Secret default unexpectedly accepted")
	}
}

func TestSecretJSONSchemaIsWriteOnlyAndHasNoDefault(t *testing.T) {
	field := Field{Name: "APIKey", JSONName: "api_key", Kind: KindSecret, Type: "secret", Tags: map[string]string{"default": "sensitive-canary"}}
	encoded, err := json.Marshal(ToJSONSchema(Schema{Name: "Request", Fields: []Field{field}}))
	if err != nil {
		t.Fatalf("marshal JSON Schema: %v", err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"writeOnly":true`) || !strings.Contains(text, `"format":"password"`) {
		t.Fatalf("Secret JSON Schema is missing write-only password metadata: %s", text)
	}
	if strings.Contains(text, "sensitive-canary") || strings.Contains(text, `"default"`) {
		t.Fatalf("Secret JSON Schema leaked or advertised a default: %s", text)
	}
}
