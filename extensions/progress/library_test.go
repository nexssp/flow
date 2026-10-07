package progress_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/progress"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestBundle_WiresLibraryAndFixtures(t *testing.T) {
	t.Parallel()

	b := progress.Bundle(nil)
	ktest.RequireEqual(t, b.ID, "progress")
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireNotNil(t, b.WrapPipeline)
	ktest.RequireNotNil(t, b.Fixtures)

	if got := b.ArgSchemas["progress.wrap"]; len(got) != 1 || got[0].Name != "action" {
		t.Fatalf("progress.wrap arg schema = %#v", got)
	}
}

func TestFixtures_Discoverable(t *testing.T) {
	t.Parallel()

	b := progress.Bundle(nil)
	var count int
	walkErr := fs.WalkDir(b.Fixtures, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".nflow") {
			return nil
		}
		content, readErr := fs.ReadFile(b.Fixtures, p)
		ktest.RequireNoError(t, readErr)
		ktest.RequireStringContains(t, string(content), "@description")
		count++
		return nil
	})
	ktest.RequireNoError(t, walkErr)
	ktest.RequireCondition(t, count > 0, "no fixtures embedded")
}

func TestPipeline_StepAndWrap(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	src := `
{ ok: true }
-> progress.step @{ message: "Validation step" }
-> progress.wrap @{ action: runtime.noop, message: "Wrapped noop" }
`
	ex, err := runner.Execute(context.Background(), cfg, src, "progress_test.nflow", nil)
	ktest.RequireNoError(t, err)

	out, ok := ex.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "output type = %T, want map", ex.Output)
	ktest.RequireEqual[any](t, out["ok"], true)
	if _, hasMessage := out["message"]; hasMessage {
		t.Errorf("message key leaked into pipeline output: %#v", out)
	}
	if _, hasAction := out["action"]; hasAction {
		t.Errorf("action key leaked into pipeline output: %#v", out)
	}
}

func TestPipeline_WrapUnknownTargetIsNotFound(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.Bundles())
	ktest.RequireNoError(t, err)

	// ArgSchemas rejects a quoted string at compile time, so this
	// must be a bare identifier that is genuinely not registered.
	src := `{ x: 1 } -> progress.wrap @{ action: does.not.exist }`
	_, err = runner.Execute(context.Background(), cfg, src, "bad.nflow", nil)
	ktest.RequireCondition(t, err != nil, "unknown target must fail")
	ktest.RequireStringContains(t, err.Error(), "unknown capability")
}
