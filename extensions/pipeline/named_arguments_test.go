package pipeline_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	assertbundle "github.com/nexssp/flow/extensions/assert"
	descriptionbundle "github.com/nexssp/flow/extensions/description"
	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	projectionbundle "github.com/nexssp/flow/extensions/projection"
	runtimebundle "github.com/nexssp/flow/extensions/runtime"
	schemabundle "github.com/nexssp/flow/extensions/schema"
	syntaxbundle "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func namedArgumentTestConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]compiler.Bundle{
		assertbundle.Bundle(nil),
		descriptionbundle.Bundle(nil),
		pipelineext.Bundle(nil),
		projectionbundle.Bundle(nil),
		runtimebundle.Bundle(nil),
		schemabundle.Bundle(nil),
		syntaxbundle.Bundle(nil),
	})
	if err != nil {
		t.Fatalf("build minimal named-argument config: %v", err)
	}
	return cfg
}

func TestNamedPipelineArgumentAdviser(t *testing.T) {
	advise := pipelineext.Bundle(nil).AtomAdvise
	if advise == nil {
		t.Fatal("pipeline bundle has no atom adviser")
	}
	for _, tc := range []struct {
		testName   string
		actionName string
		withArgs   bool
		wantNamed  bool
	}{
		{testName: "local pipeline", actionName: "pipeline.lookup", withArgs: true, wantNamed: true},
		{testName: "imported pipeline", actionName: "billing.pipeline.lookup", withArgs: true, wantNamed: true},
		{testName: "generic action", actionName: "runtime.noop", withArgs: true},
		{testName: "pipeline without argument object", actionName: "pipeline.lookup"},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			atom := &compiler.Atom{Name: tc.actionName, HasArgumentObject: tc.withArgs}
			if err := advise(atom, nil); err != nil {
				t.Fatalf("advise %q: %v", tc.actionName, err)
			}
			if atom.NamedPipelineArgs != tc.wantNamed {
				t.Errorf("NamedPipelineArgs = %t, want %t", atom.NamedPipelineArgs, tc.wantNamed)
			}
		})
	}
}

func TestNamedPipelineArgumentFixtures(t *testing.T) {
	fixtures := []string{
		"nflows/named_args_literals.nflow",
		"nflows/named_args_runtime_refs.nflow",
		"nflows/named_args_legacy_compat.nflow",
		"nflows/named_args_generic_action_compat.nflow",
	}
	cfg := namedArgumentTestConfig(t)
	for _, fixturePath := range fixtures {
		t.Run(fixturePath, func(t *testing.T) {
			source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixturePath)
			if err != nil {
				t.Fatalf("read named pipeline argument fixture %q: %v", fixturePath, err)
			}
			if _, err := runner.Execute(t.Context(), cfg, string(source), "pipeline/"+fixturePath, map[string]any{}); err != nil {
				t.Fatalf("execute named pipeline argument fixture %q: %v", fixturePath, err)
			}
		})
	}
}

func TestNamedPipelineArgumentsRespectRequestSchema(t *testing.T) {
	const fixturePath = "nflows/named_args_literals.nflow"
	source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixturePath)
	if err != nil {
		t.Fatalf("read named pipeline argument fixture %q: %v", fixturePath, err)
	}
	invalid := strings.Replace(string(source), `region: "us"`, `region: 42`, 1)
	if invalid == string(source) {
		t.Fatal("could not make fixture's named region argument invalid")
	}

	cfg := namedArgumentTestConfig(t)
	_, err = runner.Execute(t.Context(), cfg, invalid, "pipeline/named_args_invalid_request.nflow", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "LookupRequest") {
		t.Fatalf("invalid named request error = %v, want LookupRequest schema validation failure", err)
	}
}
