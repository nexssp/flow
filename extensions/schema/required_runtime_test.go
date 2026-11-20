package schema_test

import (
	"io/fs"
	"strings"
	"testing"

	schemaext "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestRequiredMarkerFixtureThroughSchemaValidateAndRequestBoundary(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(schemaext.Bundle(nil).Fixtures, "nflows/required_marker.nflow")
	if err != nil {
		t.Fatalf("read required-marker fixture: %v", err)
	}
	source := string(fixture)
	const fixtureEntry = "{ customer: { name: \"\" }, labels: [\"present\"] }\n  -> pipeline.validate_request"
	runtimeSource := strings.Replace(source, fixtureEntry, "pipeline.validate_request", 1)
	if runtimeSource == source {
		t.Fatal("required-marker fixture entrypoint not found")
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}

	input := map[string]any{
		"customer": map[string]any{"name": ""},
		"labels":   []string{"present"},
	}
	execution, err := runner.Execute(t.Context(), cfg, runtimeSource, "required-marker.nflow", input)
	if err != nil {
		t.Fatalf("execute required-marker fixture: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	customer, ok := values["customer"].(map[string]any)
	if !ok || customer["name"] != "" {
		t.Fatalf("customer = %#v, want present empty name", values["customer"])
	}
	if _, exists := values["note"]; exists {
		t.Fatal("optional note was unexpectedly added")
	}

	missingNestedName := map[string]any{
		"customer": map[string]any{},
		"labels":   []string{"present"},
	}
	_, err = runner.Execute(t.Context(), cfg, runtimeSource, "required-marker.nflow", missingNestedName)
	if err == nil || !strings.Contains(err.Error(), `field "name" is required`) {
		t.Fatalf("missing nested Customer.name error = %v, want required-field failure", err)
	}

	missingCustomer := map[string]any{"labels": []string{"present"}}
	_, err = runner.Execute(t.Context(), cfg, runtimeSource, "required-marker.nflow", missingCustomer)
	if err == nil || !strings.Contains(err.Error(), `field "customer" is required`) {
		t.Fatalf("missing Customer error = %v, want required-field failure", err)
	}
}
