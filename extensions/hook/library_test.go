package hook

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveActionWrapFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "hook")
	ktest.RequireCondition(t, b.WrapPipeline != nil, "WrapPipeline is nil")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}

func TestWrapFromMeta_NoHooks(t *testing.T) {
	t.Parallel()
	inner := ProbeAction()
	wrapped, err := wrapFromMeta(map[string]any{}, inner)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, wrapped, inner)
}

func TestWrapFromMeta_UnknownHookFails(t *testing.T) {
	t.Parallel()
	meta := map[string]any{"hooks": []string{"does.not.exist"}}
	_, err := wrapFromMeta(meta, ProbeAction())
	ktest.RequireErrorContains(t, err, "not registered")
}

func TestWrapFromMeta_KnownHook(t *testing.T) {
	t.Parallel()
	meta := map[string]any{"hooks": []string{"hook.verify"}}
	wrapped, err := wrapFromMeta(meta, ProbeAction())
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, wrapped != nil, "wrapped action is nil")
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
