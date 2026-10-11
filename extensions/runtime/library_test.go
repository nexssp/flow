package runtime

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibrary(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
	ktest.RequireCondition(t, b.SelfTest == nil, "duplicate inline SelfTest remains")
}

func TestLibrary_ActionCount(t *testing.T) {
	t.Parallel()
	lib := library()
	ktest.RequireEqual(t, len(lib.Actions), 15)
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	want := []string{
		"runtime.const", "runtime.debug", "runtime.print", "runtime.noop", "runtime.fail", "runtime.wrap", "runtime.pick",
		"runtime.with", "runtime.env", "runtime.uuid", "runtime.call", "runtime.dispatch_by_prefix", "json.clean", "runtime.sleep",
	}
	got := make(map[string]bool, len(want))
	for _, a := range library().Actions {
		if meta := a.Describe(); meta != nil {
			got[meta.Name] = true
		}
	}
	for _, name := range want {
		ktest.RequireCondition(t, got[name], "action %q missing", name)
	}
}

func TestFixturesShape(t *testing.T) {
	t.Parallel()
	fixtures := Bundle(nil).Fixtures
	count := 0
	err := fs.WalkDir(fixtures, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".nflow") {
			return nil
		}
		content, readErr := fs.ReadFile(fixtures, path)
		if readErr != nil {
			return readErr
		}
		ktest.RequireCondition(t, strings.Contains(string(content), "@description"),
			"fixture %q has no @description", path)
		count++
		return nil
	})
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, count > 0, "no NFlow fixtures")
}
