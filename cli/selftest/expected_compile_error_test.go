package selftest

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/runtime"
	flowrunner "github.com/nexssp/flow/runner"
)

func TestExpectedCompileErrorFixtures(t *testing.T) {
	t.Parallel()

	bundle := compiler.Bundle{
		ID: "expected-compile-error-fixtures",
		Fixtures: fstest.MapFS{
			"nflows/expected.nflow":   &fstest.MapFile{Data: []byte("# @expect-compile-error not registered\nruntime.missing")},
			"nflows/unexpected.nflow": &fstest.MapFile{Data: []byte("# @expect-compile-error required diagnostic\nruntime.noop")},
			"nflows/wrong.nflow":      &fstest.MapFile{Data: []byte("# @expect-compile-error absent diagnostic\nruntime.missing")},
			"nflows/normal.nflow":     &fstest.MapFile{Data: []byte("runtime.noop")},
			"nflows/syntax.nflow":     &fstest.MapFile{Data: []byte("# @syntax-only\nruntime.noop")},
		},
	}
	cfg, err := flowrunner.BuildConfig([]compiler.Bundle{runtime.Bundle(nil), bundle})
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	features := bundle.AllSelfTests()
	if len(features) != 1 || len(features[0].Features) != 5 {
		t.Fatalf("discovered sections = %#v, want one section with five fixtures", features)
	}
	results := make(map[string]Result, len(features[0].Features))
	for _, feature := range features[0].Features {
		results[feature.Name] = runSelectedFeature(t.Context(), cfg, feature, false)
	}

	for _, name := range []string{"expected", "normal", "syntax"} {
		if result := results[name]; result.Status != StatusPass {
			t.Errorf("fixture %q status = %v, want pass: %v", name, result.Status, result.Err)
		}
	}
	if result := results["unexpected"]; result.Status != StatusFail || result.Err == nil || !strings.Contains(result.Err.Error(), "compilation succeeded") {
		t.Errorf("unexpected-success result = %+v, want failure for missing compile error", result)
	}
	if result := results["wrong"]; result.Status != StatusFail || result.Err == nil || !strings.Contains(result.Err.Error(), "does not contain expected text") {
		t.Errorf("wrong-message result = %+v, want failure for diagnostic mismatch", result)
	}
	if result := results["syntax"]; !result.SyntaxOnly {
		t.Errorf("syntax-only fixture SyntaxOnly = false, want true")
	}
}
