package macros

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveAndOnPreprocess(t *testing.T) {
	b := Bundle(nil)

	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireLen(t, b.Directives, 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "macro")
	ktest.RequireNotNil(t, b.OnPreprocess)
	ktest.RequireNotNil(t, b.Fixtures)
}

func TestBundle_OnPreprocessInstallsPrimaryWithoutDeclarations(t *testing.T) {
	contributions := Bundle(nil).OnPreprocess(map[string]any{})
	ktest.RequireLen(t, contributions.Primaries, 1)

	primary, ok := contributions.Primaries[0].(*macroPrimary)
	ktest.RequireCondition(t, ok, "primary type = %T, want *macroPrimary", contributions.Primaries[0])
	ktest.RequireCondition(t, len(primary.byName) == 0, "unexpected declarations: %v", primary.byName)
}

func TestBundle_OnPreprocessIndexesDeclarations(t *testing.T) {
	want := Declaration{Name: "hi", Body: "runtime.noop", DefLine: 2, BodyLine: 3}
	contributions := Bundle(nil).OnPreprocess(map[string]any{
		DeclarationKey: []Declaration{want},
	})
	ktest.RequireLen(t, contributions.Primaries, 1)

	primary, ok := contributions.Primaries[0].(*macroPrimary)
	ktest.RequireCondition(t, ok, "primary type = %T, want *macroPrimary", contributions.Primaries[0])
	got, ok := primary.byName[want.Name]
	ktest.RequireCondition(t, ok, "declaration %q was not installed", want.Name)
	ktest.RequireEqual(t, got.Body, want.Body)
	ktest.RequireEqual(t, got.DefLine, want.DefLine)
	ktest.RequireEqual(t, got.BodyLine, want.BodyLine)
}

func TestBundle_FixturesDiscoverable(t *testing.T) {
	sections := Bundle(nil).AllSelfTests()
	ktest.RequireLen(t, sections, 1)
	ktest.RequireEqual(t, sections[0].Name, ID)
	ktest.RequireLen(t, sections[0].Features, 9)
	for _, feature := range sections[0].Features {
		ktest.RequireCondition(t, feature.Name != "", "fixture has no name: %#v", feature)
		ktest.RequireCondition(t, feature.DSL != "", "fixture %q has no DSL", feature.Name)
	}
}
