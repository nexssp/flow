package selftestkit

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
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	lib := library()
	want := []string{
		"cov.echo", "cov.double", "cov.fast", "cov.slow", "cov.flaky",
		"cov.boom", "cov.recoverable", "cov.fail.timeout", "cov.fail.unavailable",
		"cov.stub",
	}
	got := make(map[string]bool, len(lib.Actions))
	for _, a := range lib.Actions {
		got[a.Describe().Name] = true
	}
	for _, name := range want {
		ktest.RequireCondition(t, got[name], "action %q missing", name)
	}
	ktest.RequireEqual(t, len(lib.Sources), 1)
}
