package pipeline_test

import (
	"io/fs"
	"testing"

	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestSharedPipelineSchemaAliasFixture(t *testing.T) {
	const fixturePath = "nflows/schema_shared_alias.nflow"
	source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixturePath)
	if err != nil {
		t.Fatalf("read shared-schema fixture %q: %v", fixturePath, err)
	}
	if _, err := runner.Execute(t.Context(), pipelineTestConfig(t), string(source), "pipeline/"+fixturePath, nil); err != nil {
		t.Fatalf("execute shared-schema fixture %q: %v", fixturePath, err)
	}
}
