package pipeline_test

import (
	"bytes"
	"io"
	"io/fs"
	"testing"

	pipelineext "github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/runner"
)

func TestMaterializeDistributeMapForwardPipelineFixture(t *testing.T) {
	const fixturePath = "nflows/distribute_map_forward_reference.nflow"
	source, err := fs.ReadFile(pipelineext.Bundle(nil).Fixtures, fixturePath)
	if err != nil {
		t.Fatalf("read embedded pipeline fixture %q: %v", fixturePath, err)
	}

	cfg := pipelineTestConfig(t)
	var stderr bytes.Buffer
	status := runner.RunSource(t.Context(), cfg, string(source), "pipeline/"+fixturePath, nil, runner.Opts{
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if status != 0 {
		t.Fatalf("run embedded forward-reference fixture: status=%d, stderr=%s", status, stderr.String())
	}
}
