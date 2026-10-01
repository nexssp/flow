package nodes_dispatch

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestSplitMembers(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"":        nil,
		"a":       {"a"},
		"a,b,c":   {"a", "b", "c"},
		" a , b ": {"a", "b"},
		"a,,b":    {"a", "b"},
		" , , ":   nil,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, splitMembers(in), want)
		})
	}
}

func TestBundle_WiresDispatchAndFixtures(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, len(b.Libraries[0].Actions), 1)
	ktest.RequireEqual(t, b.Libraries[0].Actions[0].Describe().Name, "dispatch")
	ktest.RequireCondition(t, b.Fixtures != nil, "Fixtures is nil")
}
