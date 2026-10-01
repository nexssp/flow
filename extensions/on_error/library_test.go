package on_error

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveWrapFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "on_error")
	ktest.RequireCondition(t, b.WrapPipeline != nil, "WrapPipeline is nil")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestWrapFromMeta_NoRules(t *testing.T) {
	t.Parallel()
	var inner action.AnyAction = ErrorInfoAction
	wrapped, err := wrapFromMeta(map[string]any{}, inner)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, wrapped, inner)
}

func TestWrapFromMeta_WithRules(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"on_error": Config{ElseTarget: "noop"},
	}
	var inner action.AnyAction = ErrorInfoAction
	wrapped, err := wrapFromMeta(meta, inner)
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, wrapped != nil, "wrapped action is nil")
}

func TestLibrary_RegistersErrorInfo(t *testing.T) {
	t.Parallel()
	lib := Library()
	var found bool
	for _, a := range lib.Actions {
		if a.Describe().Name == "error.info" {
			found = true
		}
	}
	ktest.RequireCondition(t, found, "error.info action not registered")
}

func TestFixtures_Discoverable(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
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
		ktest.RequireCondition(t, strings.Contains(string(content), "@description"),
			"fixture %q has no @description", p)
		count++
		return nil
	})
	ktest.RequireNoError(t, walkErr)
	ktest.RequireCondition(t, count > 0, "no fixtures embedded")
}
