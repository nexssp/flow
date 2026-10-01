package loop

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresKeywordAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)

	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Primaries), 1)

	kw, ok := b.Primaries[0].(loopKeyword)
	if !ok {
		t.Fatalf("Primaries[0] = %T, want loopKeyword", b.Primaries[0])
	}
	ktest.RequireEqual(t, kw.Keyword(), "loop")

	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

// TestFixtures_Discoverable verifies that at least one .nflow fixture
// is embedded and that each one has an @description so the runner can
// name it.
func TestFixtures_Discoverable(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)

	var found int
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
		found++
		return nil
	})
	ktest.RequireNoError(t, walkErr)
	ktest.RequireCondition(t, found > 0, "no .nflow fixtures embedded")
}
