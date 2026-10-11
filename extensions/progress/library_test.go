package progress

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresLibraryAndFixtures(t *testing.T) {
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireLen(t, b.Libraries, 1)
	ktest.RequireNotNil(t, b.WrapPipeline)
	ktest.RequireNotNil(t, b.Fixtures)

	fields := b.ArgSchemas["progress.wrap"]
	ktest.RequireLen(t, fields, 1)
	ktest.RequireEqual(t, fields[0].Name, "action")

	sections := b.AllSelfTests()
	ktest.RequireLen(t, sections, 1)
	ktest.RequireEqual(t, sections[0].Name, ID)
	ktest.RequireLen(t, sections[0].Features, 1)
	ktest.RequireEqual(t, sections[0].Features[0].Name, "Progress step and wrap")
	ktest.RequireCondition(t, sections[0].Features[0].DSL != "", "progress fixture has no DSL")
}
