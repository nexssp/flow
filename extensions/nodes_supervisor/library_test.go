package nodes_supervisor

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestBundle_WiresSupervisor(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, b.Libraries[0].Actions[0].Describe().Name, "supervisor.run")
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestChildResult_ZeroValue(t *testing.T) {
	t.Parallel()
	var result ChildResult
	ktest.RequireEqual(t, result.TaskID, "")
	ktest.RequireEqual(t, result.Error, "")
	ktest.RequireEqual(t, result.DurationMs, int64(0))
}
