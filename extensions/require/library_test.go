package require

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresDirective(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, len(b.Directives), 1)
	ktest.RequireEqual(t, b.Directives[0].Name, "require")
}
