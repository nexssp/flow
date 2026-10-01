package fs

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibraryAndSelfTest(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestLibrary_HasSourceAndOperators(t *testing.T) {
	t.Parallel()
	lib := Library()
	ktest.RequireEqual(t, len(lib.Sources), 1)

	wantOperators := map[string]bool{
		"fs.filter": false, "fs.read": false, "fs.write": false,
		"fs.sort": false, "out.stdout": false, "out.file": false,
	}
	for _, op := range lib.Operators {
		if _, ok := wantOperators[op.Name]; ok {
			wantOperators[op.Name] = true
		}
	}
	for name, seen := range wantOperators {
		ktest.RequireCondition(t, seen, "operator %q missing", name)
	}
}

func TestSelfTestShape(t *testing.T) {
	t.Parallel()
	sections := Bundle(nil).SelfTest()
	ktest.RequireCondition(t, len(sections) > 0, "no sections")

	seen := make(map[string]bool)
	for _, s := range sections {
		ktest.RequireCondition(t, s.Name != "", "empty section name")
		for _, f := range s.Features {
			ktest.RequireCondition(t, f.Name != "", "feature has empty Name")
			ktest.RequireCondition(t, f.DSL != "", "feature %q has empty DSL", f.Name)
			ktest.RequireCondition(t, !seen[f.Name], "duplicate Name %q", f.Name)
			seen[f.Name] = true
		}
	}
}
