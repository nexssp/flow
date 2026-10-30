package pipeline_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestSymbolModifierNFlowFixturePreservesQuotedValuesAndIntegerStatus(t *testing.T) {
	t.Parallel()
	fixtures := pipelineext.Bundle(nil).Fixtures
	source, err := fs.ReadFile(fixtures, "nflows/symbol_modifiers.nflow")
	if err != nil {
		t.Fatalf("read symbol modifier fixture: %v", err)
	}
	catalog, err := fs.ReadFile(fixtures, "nflows/symbol_modifiers--catalog.nflow")
	if err != nil {
		t.Fatalf("read imported symbol fixture: %v", err)
	}
	dir := t.TempDir()
	rootFile := filepath.Join(dir, "symbol_modifiers.nflow")
	if err := os.WriteFile(filepath.Join(dir, "catalog.nflow"), catalog, 0o600); err != nil {
		t.Fatalf("write imported symbol fixture: %v", err)
	}
	compiled, err := runner.Compile(t.Context(), pipelineTestConfig(t), string(source), rootFile)
	if err != nil {
		t.Fatalf("compile symbol modifier fixture: %v", err)
	}
	for _, tc := range []struct {
		name        string
		status      int
		description string
	}{
		{name: "pipeline.local_status", status: 201, description: "local-pipeline"},
		{name: "pipeline.imported_status", status: 202, description: "imported-pipeline"},
		{name: "pipeline.quoted_literal", description: "api.labels.quoted"},
	} {
		action, ok := compiled.Resolver.Action(tc.name)
		if !ok {
			t.Fatalf("compiled resolver has no action %q", tc.name)
		}
		meta := action.Describe()
		if meta == nil || meta.SuccessStatus != tc.status || meta.Description != tc.description {
			t.Errorf("%s metadata = %#v, want status %d and description %q", tc.name, meta, tc.status, tc.description)
		}
	}
}

func TestPipelineStatusModifierResolvesLocalAndAliasedSymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	childFile := filepath.Join(dir, "http_status.nflow")
	rootFile := filepath.Join(dir, "agents.nflow")
	childSource := `@symbols http { created: "202" }
@pipeline child :status=http.created
  runtime.noop
@end
`
	if err := os.WriteFile(childFile, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `@symbols http { created: "201" }
@include "./http_status.nflow" AS api
@pipeline agents_create :status=http.created
  runtime.noop
@end
@pipeline imported_status :status=api.http.created
  runtime.noop
@end
pipeline.agents_create
`
	cfg := pipelineTestConfig(t)
	compiled, err := runner.Compile(t.Context(), cfg, source, rootFile)
	if err != nil {
		t.Fatalf("compile symbol-valued pipeline modifiers: %v", err)
	}
	for _, tc := range []struct {
		name string
		want int
	}{
		{name: "pipeline.agents_create", want: 201},
		{name: "pipeline.imported_status", want: 202},
		{name: "api.pipeline.child", want: 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			act, ok := compiled.Resolver.Action(tc.name)
			if !ok {
				t.Fatalf("compiled resolver has no action %q", tc.name)
			}
			meta := act.Describe()
			if meta == nil || meta.SuccessStatus != tc.want {
				t.Fatalf("%s status = %#v, want %d", tc.name, meta, tc.want)
			}
		})
	}
}

func TestPipelineStatusSymbolKeepsTypedValidationAndQuotedLiterals(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "resolved non-integer still fails integer validation",
			source: `@symbols http { created: "created" }
@pipeline invalid :status=http.created
  runtime.noop
@end
pipeline.invalid
`,
			want: "expected integer",
		},
		{
			name: "quoted symbol-looking string remains literal",
			source: `@symbols http { created: "201" }
@pipeline invalid :status="http.created"
  runtime.noop
@end
pipeline.invalid
`,
			want: "expected integer",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runner.Compile(t.Context(), cfg, tc.source, "modifier-validation.nflow")
			if err == nil {
				t.Fatal("compile succeeded, want typed modifier validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPipelineBoundariesUseQualifiedSchemasFromIncludedModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	moduleFile := filepath.Join(dir, "memory_types.nflow")
	rootFile := filepath.Join(dir, "chat.nflow")
	moduleSource := "@schema ChatTurn {\n  Role string `json:\"role\" validate:\"required\"`\n  Content string `json:\"content\" validate:\"required\"`\n}\n"
	if err := os.WriteFile(moduleFile, []byte(moduleSource), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `@include "./memory_types.nflow" AS memory
@pipeline memory_search
  :in-schema=!memory.ChatTurn
  runtime.noop
@end
@pipeline memory_put
  :request-schema=memory.ChatTurn
  runtime.noop
@end
@pipeline memory_response
  :out-schema=memory.ChatTurn
  runtime.noop
@end
@pipeline memory_response_invalid
  :out-schema=memory.ChatTurn
  runtime.const @{ value: { role: "user" } }
@end
@pipeline memory_validate
  schema.validate @{ name: "memory.ChatTurn" }
@end
pipeline.memory_search
`
	cfg := pipelineTestConfig(t)
	valid := map[string]any{"role": "user", "content": "hello"}
	result, err := runner.Execute(t.Context(), cfg, source, rootFile, valid)
	if err != nil {
		t.Fatalf("execute strict memory boundary with included ChatTurn: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["content"] != "hello" {
		t.Fatalf("output = %#v, want original ChatTurn map", result.Output)
	}
	_, err = runner.Execute(t.Context(), cfg, source, rootFile, map[string]any{
		"role": "user", "content": "hello", "extra": true,
	})
	if err == nil {
		t.Fatal("strict imported schema accepted an unknown field")
	}
	responseSource := strings.Replace(source, "pipeline.memory_search\n", "pipeline.memory_response\n", 1)
	responseResult, err := runner.Execute(t.Context(), cfg, responseSource, rootFile, valid)
	if err != nil {
		t.Fatalf("execute out-schema boundary with included ChatTurn: %v", err)
	}
	responseOutput, ok := responseResult.Output.(map[string]any)
	if !ok || responseOutput["content"] != "hello" {
		t.Fatalf("memory_response output = %#v, want validated ChatTurn map", responseResult.Output)
	}
	invalidResponseSource := strings.Replace(source, "pipeline.memory_search\n", "pipeline.memory_response_invalid\n", 1)
	_, err = runner.Execute(t.Context(), cfg, invalidResponseSource, rootFile, valid)
	if err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("invalid out-schema response error = %v, want missing content field", err)
	}

	validationSource := strings.Replace(source, "pipeline.memory_search\n", "pipeline.memory_validate\n", 1)
	validated, err := runner.Execute(t.Context(), cfg, validationSource, rootFile, valid)
	if err != nil {
		t.Fatalf("execute memory_validate pipeline: %v", err)
	}
	validatedOutput, ok := validated.Output.(map[string]any)
	if !ok || validatedOutput["content"] != "hello" {
		t.Fatalf("memory_validate output = %#v, want validated ChatTurn map", validated.Output)
	}

	unqualified := `@include "./memory_types.nflow" AS memory
@pipeline invalid
  :request-schema=ChatTurn
  runtime.noop
@end
pipeline.invalid
`
	_, err = runner.Compile(t.Context(), cfg, unqualified, rootFile)
	if err == nil || !strings.Contains(err.Error(), "unknown schema") {
		t.Fatalf("unqualified imported schema error = %v, want unknown schema", err)
	}

	conflicting := `@include "./memory_types.nflow" AS memory
@pipeline invalid
  :request-schema=memory.ChatTurn
  :in-schema=memory.ChatTurn
  runtime.noop
@end
pipeline.invalid
`
	_, err = runner.Compile(t.Context(), cfg, conflicting, rootFile)
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("mixed request-schema/in-schema error = %v, want duplicate spelling diagnostic", err)
	}
}

func TestPipelineRejectsMixingResponseSchemaAliases(t *testing.T) {
	t.Parallel()
	cfg := pipelineTestConfig(t)
	source := `@schema Response {
  Value string ` + "`json:\"value\"`" + `
}
@pipeline invalid
  :response-schema=Response
  :out-schema=Response
  runtime.noop
@end
pipeline.invalid
`
	_, err := runner.Compile(t.Context(), cfg, source, "mixed-response-schema-aliases.nflow")
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("mixed response-schema/out-schema error = %v, want alias conflict diagnostic", err)
	}
}
