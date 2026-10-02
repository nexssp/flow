package external

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
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestLibrary_ActionNames(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"external.exec": false, "http.request": false, "external.wasm": false}
	for _, a := range Library().Actions {
		meta := a.Describe()
		if _, ok := want[meta.Name]; ok {
			want[meta.Name] = true
		}
	}
	for name, seen := range want {
		ktest.RequireCondition(t, seen, "action %q missing", name)
	}
}

func TestExecResult_ZeroValue(t *testing.T) {
	t.Parallel()
	// The result type must be safe to construct as a zero value —
	// callers and JSON paths rely on this.
	var result ExecResult
	ktest.RequireEqual(t, result.ExitCode, 0)
	ktest.RequireEqual(t, result.OK, false)
	ktest.RequireEqual(t, result.Output, nil)
}
