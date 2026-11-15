package pipeline_test

import (
	"io/fs"
	"strings"
	"testing"

	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestPipelineStaticBindingConflictNFlowFixture(t *testing.T) {
	t.Parallel()

	const fixture = "nflows/ambiguous_binding_static.nflow"
	source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixture)
	if err != nil {
		t.Fatalf("read fixture %q: %v", fixture, err)
	}
	_, err = runner.Compile(t.Context(), pipelineTestConfig(t), string(source), fixture)
	if err == nil || !strings.Contains(err.Error(), `ambiguous bare identifier "tool"`) {
		t.Fatalf("compile fixture error = %v, want static input/binding ambiguity", err)
	}
	for _, want := range []string{"input field \"tool\"", "pipeline `as tool` binding"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("compile diagnostic %q does not contain %q", err, want)
		}
	}
}
