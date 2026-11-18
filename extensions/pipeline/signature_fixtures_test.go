package pipeline_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestPipelineSignatureFixturesExecute(t *testing.T) {
	cfg := pipelineTestConfig(t)
	fixtures := pipelineext.Bundle(nil).Fixtures
	for _, tc := range []struct {
		name  string
		input map[string]any
		want  any
	}{
		{name: "signature_typed_input_output.nflow", input: map[string]any{"name": "Ada"}, want: map[string]any{"name": "Ada"}},
		{name: "signature_input_only.nflow", input: map[string]any{"name": "Lin"}, want: map[string]any{"name": "Lin"}},
		{name: "signature_output_only.nflow", want: map[string]any{"name": "created"}},
		{name: "signature_untyped_legacy.nflow", input: map[string]any{"name": "legacy", "extra": true}, want: map[string]any{"name": "legacy", "extra": true}},
		{name: "signature_compatible_legacy.nflow", input: map[string]any{"name": "dual"}, want: map[string]any{"name": "dual"}},
		{name: "signature_duplicate_legacy_identical.nflow", input: map[string]any{"name": "repeat"}, want: map[string]any{"name": "repeat"}},
		{name: "signature_profile_local_override.nflow", want: map[string]any{"name": "Ada"}},
		{name: "signature_local_profile_override.nflow", want: map[string]any{"name": "Lin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := fs.ReadFile(fixtures, "nflows/"+tc.name)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			result, err := runner.Execute(t.Context(), cfg, string(source), tc.name, tc.input)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !equalFixtureValue(result.Output, tc.want) {
				t.Fatalf("output = %#v, want %#v", result.Output, tc.want)
			}
		})
	}
}

func TestPipelineSignatureConflictFixturesFailCompilation(t *testing.T) {
	cfg := pipelineTestConfig(t)
	fixtures := pipelineext.Bundle(nil).Fixtures
	for _, tc := range []struct {
		name string
		want string
		line int
	}{
		{name: "signature_conflicting_input_schema.nflow", want: "conflicting input contract declarations", line: 8},
		{name: "signature_conflicting_input_strictness.nflow", want: "conflicting input contract declarations", line: 5},
		{name: "signature_conflicting_output_strictness.nflow", want: "conflicting output contract declarations", line: 5},
		{name: "signature_duplicate_legacy_conflict.nflow", want: "conflicting duplicate :request-schema declarations", line: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := fs.ReadFile(fixtures, "nflows/"+tc.name)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			_, err = runner.Compile(t.Context(), cfg, string(source), tc.name)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compile() error = %v, want substring %q", err, tc.want)
			}
			position, ok := compiler.PositionOf(err)
			if !ok || position.File != tc.name || position.Line != tc.line {
				t.Fatalf("Compile() position = %+v, positioned = %t; want %s:%d", position, ok, tc.name, tc.line)
			}
		})
	}
}

func TestUnknownPipelineSignatureSchemaErrorHasDeclarationPosition(t *testing.T) {
	cfg := pipelineTestConfig(t)
	const source = `@pipeline invalid(req: MissingRequest)
  noop
@end
pipeline.invalid
`
	const file = "unknown-signature-schema.nflow"
	_, err := runner.Compile(t.Context(), cfg, source, file)
	if err == nil || !strings.Contains(err.Error(), "unknown schema") {
		t.Fatalf("Compile() error = %v, want unknown schema error", err)
	}
	position, ok := compiler.PositionOf(err)
	if !ok || position.File != file || position.Line != 1 {
		t.Fatalf("Compile() position = %+v, positioned = %t; want %s:1", position, ok, file)
	}
}

func TestPipelineSignatureStrictBoundariesRejectUnknownFields(t *testing.T) {
	cfg := pipelineTestConfig(t)
	fixtures := pipelineext.Bundle(nil).Fixtures
	source, err := fs.ReadFile(fixtures, "nflows/signature_typed_input_output.nflow")
	if err != nil {
		t.Fatalf("read typed signature fixture: %v", err)
	}
	invalidInput := strings.Replace(string(source), `{ name: "Ada" }`, `{ name: "Ada", extra: true }`, 1)
	_, err = runner.Execute(t.Context(), cfg, invalidInput, "strict-signature-input.nflow", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("strict signature input error = %v, want unknown fields rejection", err)
	}

	invalidResponse := strings.Replace(string(source), "  noop\n", "  const @{ value: { name: \"Ada\", extra: true } }\n", 1)
	_, err = runner.Execute(t.Context(), cfg, invalidResponse, "strict-signature-output.nflow", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("strict signature output error = %v, want unknown fields rejection", err)
	}
}

func TestPipelineSignatureInputSeedsTypedBodyAnalysis(t *testing.T) {
	cfg := pipelineTestConfig(t)
	const prefix = `@schema Request {
  Task string ` + "`json:\"task\"`" + `
}
@pipeline typed(req: !Request)
  noop as request
  -> { value: request.task }
@end
`
	if _, err := runner.Compile(t.Context(), cfg, prefix+"pipeline.typed\n", "typed-signature-analysis.nflow"); err != nil {
		t.Fatalf("Compile() valid typed input reference: %v", err)
	}
	invalid := strings.Replace(prefix, "request.task", "request.missing", 1)
	if _, err := runner.Compile(t.Context(), cfg, invalid+"pipeline.typed\n", "typed-signature-analysis-invalid.nflow"); err == nil {
		t.Fatal("Compile() accepted a body reference missing from the signature's input schema")
	}
}

func equalFixtureValue(got, want any) bool {
	gotMap, gotIsMap := got.(map[string]any)
	wantMap, wantIsMap := want.(map[string]any)
	if !gotIsMap || !wantIsMap {
		return got == want
	}
	if len(gotMap) != len(wantMap) {
		return false
	}
	for key, value := range wantMap {
		if gotMap[key] != value {
			return false
		}
	}
	return true
}
