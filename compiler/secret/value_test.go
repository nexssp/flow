package secret

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type testRequest struct {
	APIKey  Value  `json:"api_key"`
	Message string `json:"message"`
}

func TestValueDoesNotFormatOrSerializeContents(t *testing.T) {
	value := New("sensitive-canary")
	if got := fmt.Sprint(value); got != Redacted {
		t.Fatalf("fmt.Sprint(secret) = %q, want %q", got, Redacted)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(secret): %v", err)
	}
	if got := string(encoded); got != `"[REDACTED]"` {
		t.Fatalf("JSON secret = %s, want redaction marker", got)
	}
	var decoded Value
	if err := json.Unmarshal([]byte(`"sensitive-canary"`), &decoded); err == nil {
		t.Fatal("JSON unmarshal unexpectedly created a Secret from a source literal")
	}
}

func TestFitsTypeRequiresNominalSecretField(t *testing.T) {
	target := reflect.TypeFor[testRequest]()
	if !FitsType(target, target) {
		t.Fatal("identical typed Secret request should fit")
	}
	if FitsType(target, reflect.TypeFor[map[string]any]()) {
		t.Fatal("Secret request unexpectedly fits an untyped map")
	}
	if got := WithoutSecrets(target); got == target || TypeContains(got) {
		t.Fatalf("WithoutSecrets(%v) = %v, want a distinct secret-free type", target, got)
	}
	if !FitsValue(map[string]any{"api_key": New("sensitive-canary")}, target) {
		t.Fatal("map payload with the exact typed Secret field should fit the schema request")
	}
	if FitsValue(map[string]any{"api_key": "sensitive-canary"}, target) {
		t.Fatal("plain string unexpectedly fits a typed Secret field")
	}
}

func TestSanitizeWithSourcesRedactsTypedSecretAndEcho(t *testing.T) {
	const token = "sensitive-canary"
	input := map[string]any{"api_key": New(token)}
	output := map[string]any{"body": "server echoed: " + token}
	got, err := json.Marshal(SanitizeWithSources(output, input))
	if err != nil {
		t.Fatalf("marshal sanitized event: %v", err)
	}
	if strings.Contains(string(got), token) || !strings.Contains(string(got), Redacted) {
		t.Fatalf("sanitized event leaked or omitted marker: %s", got)
	}
	sanitized, ok := SanitizeWithSources(input, input).(map[string]any)
	if !ok {
		t.Fatalf("sanitized Secret payload type = %T, want map[string]any", SanitizeWithSources(input, input))
	}
	if got := sanitized["api_key"]; got != Redacted {
		t.Fatalf("typed Secret event field = %#v, want %q", got, Redacted)
	}
}

func TestSanitizeErrorRedactsSecretContents(t *testing.T) {
	const token = "sensitive-canary"
	err := SanitizeError(fmt.Errorf("request failed with %s", token), New(token))
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), Redacted) {
		t.Fatalf("sanitized error = %v, want the token redacted", err)
	}
}

func TestSanitizeBoundaryRedactsRawWireSecretAndEchoes(t *testing.T) {
	const token = "sensitive-canary"
	request := map[string]any{"api_key": token, "message": "safe"}
	sanitized, values := SanitizeBoundary(request, reflect.TypeFor[testRequest]())
	fields, ok := sanitized.(map[string]any)
	if !ok {
		t.Fatalf("sanitized request type = %T, want map[string]any", sanitized)
	}
	if fields["api_key"] != Redacted || fields["message"] != "safe" {
		t.Fatalf("sanitized request = %#v, want redacted key and preserved safe field", fields)
	}
	if len(values) != 1 || values[0] != token {
		t.Fatalf("extracted secret values = %#v, want the one schema Secret", values)
	}
	response := SanitizeWithSecretValues(map[string]any{"body": "server echoed " + token}, values...)
	if strings.Contains(fmt.Sprint(response), token) || !strings.Contains(fmt.Sprint(response), Redacted) {
		t.Fatalf("sanitized response leaked the schema Secret: %#v", response)
	}
	err := SanitizeErrorWithSecretValues(fmt.Errorf("failure echoed %s", token), values...)
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), Redacted) {
		t.Fatalf("sanitized wire Secret error = %v", err)
	}
}

func TestSanitizeWithSecretValuesFailsClosedForUnmarshalableData(t *testing.T) {
	const token = "sensitive-canary"
	cycle := map[string]any{"body": token}
	cycle["cycle"] = cycle
	if got := SanitizeWithSecretValues(cycle, token); got != Redacted {
		t.Fatalf("unmarshalable secret-bearing payload = %#v, want redaction marker", got)
	}
}

func TestScrubberPreservesNonSecretPointerWhenEmpty(t *testing.T) {
	type output struct {
		Message string
	}
	value := &output{Message: "ordinary result"}
	scrubbed := NewScrubber().Sanitize(value)
	got, ok := scrubbed.(*output)
	if !ok {
		t.Fatalf("scrubbed output type = %T, want *output", scrubbed)
	}
	if got != value {
		t.Fatalf("scrubbed pointer = %p, want original %p", got, value)
	}
}

func TestScrubberCapturesAndClearsTypedSecretValues(t *testing.T) {
	const token = "sensitive-canary"
	scrubber := NewScrubber()
	scrubber.Capture(map[string]any{"api_key": New(token)}, nil)
	output := scrubber.Sanitize(map[string]any{"body": "server echoed " + token})
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal scrubbed output: %v", err)
	}
	if strings.Contains(string(encoded), token) || !strings.Contains(string(encoded), Redacted) {
		t.Fatalf("scrubbed output leaked or omitted marker: %s", encoded)
	}
	scrubber.Clear()
	if got := scrubber.Sanitize("ordinary value " + token); got != "ordinary value "+token {
		t.Fatalf("cleared scrubber retained values: %#v", got)
	}
}
