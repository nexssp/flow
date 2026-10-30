package symbols_test

import (
	"io/fs"
	"testing"
	"time"

	symbolsext "github.com/nexssp/flow/extensions/symbols"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestTypedSymbolsRuntimeFixtureKeepsNativeAndLegacyValues(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(symbolsext.Bundle(nil).Fixtures, "nflows/typed_values.nflow")
	if err != nil {
		t.Fatalf("read typed symbol fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, string(fixture), "typed-values.nflow", nil)
	if err != nil {
		t.Fatalf("execute typed symbol fixture: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	for key, want := range map[string]any{
		"status":   201,
		"base_url": "https://api.internal",
		"timeout":  5 * time.Second,
		"legacy":   "needs_review",
		"identity": "pending",
	} {
		if values[key] != want {
			t.Errorf("%s = %#v (%T), want %#v (%T)", key, values[key], values[key], want, want)
		}
	}
}
