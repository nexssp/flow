package schema_test

import (
	"io/fs"
	"testing"
	"time"

	schemaext "github.com/nexssp/flow/extensions/schema"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestTypedSchemaFixtureAppliesNativeDefaultsAndValidation(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(schemaext.Bundle(nil).Fixtures, "nflows/typed_symbol_defaults.nflow")
	if err != nil {
		t.Fatalf("read typed schema fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, string(fixture), "typed-schema-defaults.nflow", nil)
	if err != nil {
		t.Fatalf("execute typed schema fixture: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	for key, want := range map[string]any{
		"status":  201,
		"timeout": 5 * time.Second,
		"label":   "created",
	} {
		if values[key] != want {
			t.Errorf("%s = %#v (%T), want %#v (%T)", key, values[key], values[key], want, want)
		}
	}
}
