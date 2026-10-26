package lang

import (
	"testing"
)

// TestAliasUnknownTag ensures unknown tags return ok=false rather than panicking.
func TestAliasUnknownTag(t *testing.T) {
	for _, tag := range []string{"", "cobol", "pascal", "PY", "Python"} {
		if _, _, ok := resolveAlias(tag); ok {
			t.Errorf("resolveAlias(%q) returned ok=true for unknown tag", tag)
		}
	}
}

// TestAliasDerivedFromRunners asserts the alias table is derived from the
// runner files, not hand-maintained. After LoadRunners, every alias
// resolves to its runner's (ID, Version).
func TestAliasDerivedFromRunners(t *testing.T) {
	for k := range Registry {
		delete(Registry, k)
	}
	aliases = nil
	if err := LoadRunners(); err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}

	// Every alias in the derived table must point at a registered language.
	for _, a := range aliases {
		if _, err := Lookup(a.ID, a.Version); err != nil {
			t.Errorf("alias %q -> %s@%q not registered: %v", a.Tag, a.ID, a.Version, err)
		}
	}
}

// TestAliasDuplicatePanics asserts two runner files claiming the same alias
// panic at registration. Silent override would let a stale entry win.
func TestAliasDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("registerAlias with duplicate tag did not panic")
		}
	}()
	registerAlias("py", "python", "") // already registered by LoadRunners
	registerAlias("py", "rust", "")   // should panic
}

// TestAllAliasesSorted asserts allAliases returns sorted-by-tag output.
// Stable output is needed for `nflow explain lang` and deterministic tests.
func TestAllAliasesSorted(t *testing.T) {
	got := allAliases()
	for i := 1; i < len(got); i++ {
		if got[i-1].Tag > got[i].Tag {
			t.Errorf("allAliases not sorted: %q before %q", got[i-1].Tag, got[i].Tag)
		}
	}
}
