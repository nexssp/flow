package core_test

import (
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// uniqueID returns a name unlikely to collide with another test's
// registration. The bundle registry is process-global, so tests that
// call Register must not use generic names — t.Name() is unique per
// test and survives -run filters.
func uniqueID(t *testing.T) string {
	t.Helper()
	return "test_" + t.Name()
}

// ── Register / Lookup ────────────────────────────────────────────────

func TestRegister_Lookup(t *testing.T) {
	id := uniqueID(t)

	called := false
	core.Register(id, func(_ map[string]string) core.Bundle {
		called = true
		return core.Bundle{ID: id}
	})

	factory, ok := core.Lookup(id)
	ktest.RequireTrue(t, ok)
	ktest.RequireNotNil(t, factory)

	b := factory(nil)
	ktest.RequireTrue(t, called)
	ktest.RequireEqual(t, b.ID, id)
}

func TestRegister_NilFactory_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.Register(uniqueID(t), nil)
	})
}

func TestRegister_EmptyID_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.Register("", func(_ map[string]string) core.Bundle {
			return core.Bundle{ID: "x"}
		})
	})
}

func TestRegister_WhitespaceID_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.Register("   ", func(_ map[string]string) core.Bundle {
			return core.Bundle{ID: "x"}
		})
	})
}

func TestRegister_DuplicateID_Panics(t *testing.T) {
	id := uniqueID(t)
	core.Register(id, func(_ map[string]string) core.Bundle {
		return core.Bundle{ID: id}
	})

	ktest.RequirePanics(t, func() {
		core.Register(id, func(_ map[string]string) core.Bundle {
			return core.Bundle{ID: id}
		})
	})
}

func TestLookup_Missing(t *testing.T) {
	_, ok := core.Lookup("this_id_is_never_registered_by_any_test")
	ktest.RequireFalse(t, ok)
}

// ── LookupBundleForModule ────────────────────────────────────────────

func TestLookupBundleForModule_TrailingNexssflowSegment(t *testing.T) {
	id := uniqueID(t)
	core.Register(id, func(_ map[string]string) core.Bundle {
		return core.Bundle{ID: id}
	})

	// Convention: github.com/org/repo/nexssflow resolves to the bundle
	// registered under the parent directory's base name.
	_, ok := core.LookupBundleForModule("github.com/example/" + id + "/nexssflow")
	ktest.RequireTrue(t, ok)
}

func TestLookupBundleForModule_DirectMatch(t *testing.T) {
	id := uniqueID(t)
	core.Register(id, func(_ map[string]string) core.Bundle {
		return core.Bundle{ID: id}
	})

	_, ok := core.LookupBundleForModule("github.com/example/" + id)
	ktest.RequireTrue(t, ok)
}

func TestLookupBundleForModule_BackslashSeparators(t *testing.T) {
	id := uniqueID(t)
	core.Register(id, func(_ map[string]string) core.Bundle {
		return core.Bundle{ID: id}
	})

	// Windows-style paths are normalized before lookup.
	_, ok := core.LookupBundleForModule(`github.com\example\` + id + `\nexssflow`)
	ktest.RequireTrue(t, ok)
}

func TestLookupBundleForModule_Missing(t *testing.T) {
	_, ok := core.LookupBundleForModule("github.com/nobody/never-registered")
	ktest.RequireFalse(t, ok)
}

// ── ValidateBundle ───────────────────────────────────────────────────

func TestValidateBundle_EmptyID_Fails(t *testing.T) {
	err := core.ValidateBundle(core.Bundle{})
	ktest.RequireErrorContains(t, err, "ID is required")
}

func TestValidateBundle_InvalidAlias_Fails(t *testing.T) {
	err := core.ValidateBundle(core.Bundle{ID: "x", Alias: "1invalid"})
	ktest.RequireErrorContains(t, err, "invalid @require namespace")
}

func TestValidateBundle_DuplicateLibraryName_Fails(t *testing.T) {
	err := core.ValidateBundle(core.Bundle{
		ID: "x",
		Libraries: []action.Library{
			{Name: "a"},
			{Name: "a"},
		},
	})
	ktest.RequireErrorContains(t, err, "duplicate library")
}

func TestValidateBundle_MinimalValid_Passes(t *testing.T) {
	err := core.ValidateBundle(core.Bundle{ID: "x"})
	ktest.RequireNoError(t, err)
}
