package core_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

func TestTranslateKeyword(t *testing.T) {
	tests := []struct {
		in     string
		target string
		ok     bool
	}{
		{"const", "runtime.const", true},
		{"noop", "runtime.noop", true},
		{"json", "json.clean", true},
		{"parallel", "", false},
		{"runtime.const", "", false},
		{"http.request", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		target, ok := core.TranslateKeyword(tc.in)
		ktest.RequireEqual(t, ok, tc.ok)
		ktest.RequireEqual(t, target, tc.target)
	}
}

func TestIsReservedActionName(t *testing.T) {
	ktest.RequireCondition(t, core.IsReservedActionName("const"), "const should be reserved")
	ktest.RequireCondition(t, core.IsReservedActionName("noop"), "noop should be reserved")
	ktest.RequireCondition(t, core.IsReservedActionName("parallel"), "parallel should be reserved")
	ktest.RequireCondition(t, !core.IsReservedActionName("runtime.const"), "runtime.const should not be reserved")
	ktest.RequireCondition(t, !core.IsReservedActionName("http.request"), "http.request should not be reserved")
}

func TestRequiredModifierOwner(t *testing.T) {
	ktest.RequireEqual(t, core.RequiredModifierOwner("timeout"), core.OwnerKernel)
	ktest.RequireEqual(t, core.RequiredModifierOwner("retry"), core.OwnerKernel)
	ktest.RequireEqual(t, core.RequiredModifierOwner("profile"), core.OwnerGrammar)
	ktest.RequireEqual(t, core.RequiredModifierOwner("model"), core.OwnerBundle)
}
