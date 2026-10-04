package nodes_dispatch

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestBundle_DeclaresCapabilityReferenceListForMembers(t *testing.T) {
	t.Parallel()
	schema := Bundle(nil).ArgSchemas["dispatch.run"]
	for _, field := range schema {
		if field.Name == "members" {
			ktest.RequireEqual(t, field.Kind, core.ArgCapabilityRefList)
			ktest.RequireCondition(t, !field.Optional, "dispatch.run.members must be required")
			return
		}
	}
	t.Fatal("dispatch.run schema has no members field")
}

func TestBundle_WiresDispatchAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, b.Libraries[0].Actions[0].Describe().Name, "dispatch.run")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}
