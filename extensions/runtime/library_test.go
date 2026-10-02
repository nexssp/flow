package runtime

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibrary(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestLibrary_ActionCount(t *testing.T) {
	t.Parallel()
	lib := library()
	ktest.RequireEqual(t, len(lib.Actions), 13)
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	want := []string{
		"runtime.const", "runtime.debug", "runtime.noop", "runtime.fail", "runtime.wrap", "runtime.pick",
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
