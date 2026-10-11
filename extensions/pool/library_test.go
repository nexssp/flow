package pool

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirectiveMaterializerAndFixtures(t *testing.T) {
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireLen(t, b.Libraries, 1)
	ktest.RequireLen(t, b.Directives, 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "pool")
	ktest.RequireNotNil(t, b.Materialize)
	ktest.RequireNotNil(t, b.Fixtures)
}

func TestBundle_FixturesDiscoverable(t *testing.T) {
	sections := Bundle(nil).AllSelfTests()
	ktest.RequireLen(t, sections, 1)
	ktest.RequireEqual(t, sections[0].Name, ID)
	ktest.RequireLen(t, sections[0].Features, 2)
	for _, feature := range sections[0].Features {
		ktest.RequireCondition(t, feature.Name != "", "fixture has no name: %#v", feature)
		ktest.RequireCondition(t, feature.DSL != "", "fixture %q has no DSL", feature.Name)
	}
}
