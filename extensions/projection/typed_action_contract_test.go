package projection_test

import (
	"io/fs"
	"strings"
	"testing"

	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestProjectionTypedJSONAndSchemaActionContracts(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixtures := projectionext.Bundle(nil).Fixtures
	for _, tc := range []struct {
		name  string
		want  string
		input map[string]any
	}{
		{name: "typed_json_value_input.nflow", want: "object"},
		{name: "typed_json_value_scalar_input.nflow", want: "scalar"},
		{name: "typed_pointer_float32_to_float32.nflow", want: "0.75", input: map[string]any{"score": 0.75}},
		{name: "typed_schema_string_input.nflow", want: "memory note", input: map[string]any{"value": "memory note"}},
		{name: "typed_schema_uint64_input.nflow", want: "7", input: map[string]any{"count": uint64(7)}},
		{name: "typed_schema_uint32_to_uint64_input.nflow", want: "7", input: map[string]any{"count": uint32(7)}},
		{name: "typed_argument_expression_valid.nflow", want: "Julia", input: map[string]any{"name": "Julia"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := fs.ReadFile(fixtures, "nflows/"+tc.name)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			result, err := runner.Execute(t.Context(), cfg, string(fixture), tc.name, tc.input)
			if err != nil {
				t.Fatalf("execute typed contract fixture: %v", err)
			}
			if result.Output != tc.want {
				t.Fatalf("typed action output = %#v, want %q", result.Output, tc.want)
			}
		})
	}
}

func TestProjectionTypedActionInputContracts(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixtures := projectionext.Bundle(nil).Fixtures

	valid, err := fs.ReadFile(fixtures, "nflows/typed_action_input_valid.nflow")
	if err != nil {
		t.Fatalf("read valid fixture: %v", err)
	}
	result, err := runner.Execute(t.Context(), cfg, string(valid), "typed_action_input_valid.nflow", nil)
	if err != nil {
		t.Fatalf("execute compatible projected request: %v", err)
	}
	if result.Output != "Hello Julia" {
		t.Fatalf("compatible projected request output = %#v, want %q", result.Output, "Hello Julia")
	}
}

func TestProjectionTypedActionContractCompileErrors(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixtures := projectionext.Bundle(nil).Fixtures
	for _, name := range []string{
		"typed_action_input_invalid.nflow",
		"typed_json_value_array_input.nflow",
		"typed_schema_dynamic_to_string_invalid.nflow",
		"typed_schema_int_to_uint64_invalid.nflow",
		"typed_argument_expression_invalid.nflow",
		"typed_argument_spread_expression_invalid.nflow",
	} {
		t.Run(name, func(t *testing.T) {
			fixture, err := fs.ReadFile(fixtures, "nflows/"+name)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			want := expectedCompileError(t, string(fixture))
			_, err = runner.Compile(t.Context(), cfg, string(fixture), name)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("compile error = %v, want diagnostic containing %q", err, want)
			}
		})
	}
}

func expectedCompileError(t *testing.T, source string) string {
	t.Helper()
	const prefix = "# @expect-compile-error "
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if diagnostic, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(diagnostic)
		}
	}
	t.Fatal("fixture is missing its @expect-compile-error diagnostic")
	return ""
}
