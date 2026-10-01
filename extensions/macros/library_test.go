package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveAndOnPreprocess(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)

	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "macro")
	ktest.RequireCondition(t, b.OnPreprocess != nil, "OnPreprocess is nil")
}

func TestBundle_OnPreprocess_NoDeclarations(t *testing.T) {
	t.Parallel()
	contribs := Bundle(nil).OnPreprocess(map[string]any{})
	ktest.RequireEqual(t, len(contribs.Primaries), 0)
}

func TestBundle_OnPreprocess_WithDeclarations(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		DeclarationKey: []Declaration{
			{Name: "a", Body: "noop"},
			{Name: "b", Body: "debug"},
		},
	}
	contribs := Bundle(nil).OnPreprocess(meta)
	ktest.RequireEqual(t, len(contribs.Primaries), 1)

	primary, ok := contribs.Primaries[0].(*macroPrimary)
	if !ok {
		t.Fatalf("got %T, want *macroPrimary", contribs.Primaries[0])
	}
	ktest.RequireEqual(t, len(primary.byName), 2)
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
