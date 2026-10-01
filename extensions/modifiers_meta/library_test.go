package modifiers_meta

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresModifiers(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)

	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")

	want := []string{
		"name", "desc", "description", "status", "tag", "scope",
		"read_only", "audit", "debug", "deprecated", "strict", "lenient",
	}
	got := make(map[string]bool, len(b.Modifiers))
	for _, m := range b.Modifiers {
		got[m.Name] = true
	}
	for _, name := range want {
		ktest.RequireCondition(t, got[name], "modifier %q missing", name)
	}
}

func TestBundle_SelfTestShape(t *testing.T) {
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
