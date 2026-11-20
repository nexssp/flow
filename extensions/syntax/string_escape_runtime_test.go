package syntax_test

import (
	"io/fs"
	"testing"

	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestStringEscapeRuntimeFixture(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(syntaxext.Bundle(nil).Fixtures, "nflows/string_escape_data.nflow")
	if err != nil {
		t.Fatalf("read string escape fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("build native config: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, string(fixture), "string-escape-runtime.nflow", nil)
	if err != nil {
		t.Fatalf("execute string escape fixture: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output = %T (%#v), want map[string]any", execution.Output, execution.Output)
	}
	for key, want := range map[string]any{
		"line":               "line\nbreak",
		"expression":         "line\nbreak",
		"raw":                `line\nbreak`,
		"unicode":            "A",
		"unicode_expression": "A",
		"byte":               "ÿ",
		"action_line":        "line\nbreak",
		"action_unicode":     "A",
		"action_byte":        "ÿ",
		"action_raw":         `line\nbreak`,
		"action_single":      `line\nbreak`,
	} {
		if values[key] != want {
			t.Errorf("%s = %#v, want %#v", key, values[key], want)
		}
	}
}
