package runner_test

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestFailedActionAliasIsRejectedInRecoveryActionArgument(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	source := `runtime.fail ( kind: "Unavailable", message: "expected failure" ) as failed
? on_error {
  xerr.KindUnavailable -> runtime.noop ( value: failed )
}`
	_, err = runner.Execute(t.Context(), cfg, source, "failed-binding-argument.nflow", nil)
	if err == nil {
		t.Fatal("Execute() succeeded; want a failed-binding error rather than nil argument evaluation")
	}
	if !strings.Contains(err.Error(), `pipeline binding "failed" is unavailable because its action failed`) {
		t.Fatalf("Execute() error = %v, want unavailable-binding error", err)
	}
}
