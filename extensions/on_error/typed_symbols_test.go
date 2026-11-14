package on_error_test

import (
	"io/fs"
	"testing"

	onerrorext "github.com/nexssp/flow/extensions/on_error"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestTypedHandlerCanReferenceFileLocalSymbols(t *testing.T) {
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	source, err := fs.ReadFile(onerrorext.Bundle(nil).Fixtures, "nflows/typed_local_symbols.nflow")
	if err != nil {
		t.Fatalf("read typed local-symbol fixture: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, string(source), "typed_local_symbols.nflow"); err != nil {
		t.Fatalf("compile typed local-symbol handler: %v", err)
	}
	execution, err := runner.Execute(t.Context(), cfg, string(source), "typed_local_symbols.nflow", nil)
	if err != nil {
		t.Fatalf("execute typed local-symbol handler: %v", err)
	}
	values, ok := execution.Output.(map[string]any)
	if !ok || values["timeout_code"] != 504 {
		t.Fatalf("recovered output = %#v, want timeout_code=504", execution.Output)
	}
}
