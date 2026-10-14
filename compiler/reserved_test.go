package compiler_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
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
		target, ok := compiler.TranslateKeyword(tc.in)
		ktest.RequireEqual(t, ok, tc.ok)
		ktest.RequireEqual(t, target, tc.target)
	}
}

func TestIsReservedActionName(t *testing.T) {
	ktest.RequireCondition(t, compiler.IsReservedActionName("const"), "const should be reserved")
	ktest.RequireCondition(t, compiler.IsReservedActionName("noop"), "noop should be reserved")
	ktest.RequireCondition(t, compiler.IsReservedActionName("parallel"), "parallel should be reserved")
	ktest.RequireCondition(t, compiler.IsReservedActionName("race"), "race should be reserved")
	ktest.RequireCondition(t, !compiler.IsReservedActionName("runtime.const"), "runtime.const should not be reserved")
	ktest.RequireCondition(t, !compiler.IsReservedActionName("http.request"), "http.request should not be reserved")
}

func TestRequiredModifierOwner(t *testing.T) {
	ktest.RequireEqual(t, compiler.RequiredModifierOwner("timeout"), compiler.OwnerKernel)
	ktest.RequireEqual(t, compiler.RequiredModifierOwner("retry"), compiler.OwnerKernel)
	ktest.RequireEqual(t, compiler.RequiredModifierOwner("profile"), compiler.OwnerGrammar)
	ktest.RequireEqual(t, compiler.RequiredModifierOwner("model"), compiler.OwnerBundle)
}
