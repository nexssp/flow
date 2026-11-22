package syntax_test

import (
	"io/fs"
	"testing"

	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

type definedConditionalBool bool

func TestTernaryRoutesDefinedBoolFalseToElse(t *testing.T) {
	fixture, err := fs.ReadFile(syntaxext.Bundle(nil).Fixtures, "nflows/ternary_defined_bool.nflow")
	if err != nil {
		t.Fatalf("read ternary fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}

	result, err := runner.Execute(t.Context(), cfg, string(fixture), "ternary_defined_bool.nflow", map[string]any{
		"condition": definedConditionalBool(false),
	})
	if err != nil {
		t.Fatalf("execute ternary fixture: %v", err)
	}
	if result.Output != "false" {
		t.Fatalf("ternary output = %#v, want false branch", result.Output)
	}
}
