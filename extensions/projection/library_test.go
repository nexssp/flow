package projection

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresPrimaryLibraryFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Primaries), 1)
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")

	primary, ok := b.Primaries[0].(Extension)
	ktest.RequireCondition(t, ok, "Primaries[0] = %T, want Extension", b.Primaries[0])
	ktest.RequireEqual(t, primary.Name(), "projection")
}

func TestLibrary_NilEvaluatorUsesDefault(t *testing.T) {
	t.Parallel()
	lib := Library(nil)
	ktest.RequireEqual(t, len(lib.Actions), 1)
	ktest.RequireEqual(t, lib.Actions[0].Describe().Name, "projection")
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
	ktest.RequireCondition(t, count >= 4, "expected at least 4 fixtures, got %d", count)
}
