package projection_test

import (
	"io/fs"
	"strings"
	"testing"

	projectionext "github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

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
	if result.Output != "Hello Ada" {
		t.Fatalf("compatible projected request output = %#v, want %q", result.Output, "Hello Ada")
	}

	invalid, err := fs.ReadFile(fixtures, "nflows/typed_action_input_invalid.nflow")
	if err != nil {
		t.Fatalf("read invalid fixture: %v", err)
	}
	_, err = runner.Compile(t.Context(), cfg, string(invalid), "typed_action_input_invalid.nflow")
	if err == nil || !strings.Contains(err.Error(), "contract mismatch pipeline value -> template.render") {
		t.Fatalf("compile incompatible projected request = %v, want a typed action input contract error", err)
	}
}
