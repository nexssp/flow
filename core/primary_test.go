package core_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
)

// ── stubs ─────────────────────────────────────────────────────────────

// stubToken is a minimal TokenPrimary on TokAtPrompt. Parse is never
// invoked from these tests; it exists only to satisfy the interface.
type stubToken struct{ tag string }

func (s stubToken) Name() string                        { return s.tag }
func (stubToken) TokenType() core.TokenType             { return core.TokAtPrompt }
func (stubToken) Parse(*core.Parser) (core.Expr, error) { return nil, nil }

// stubMergeable appends the tags it has seen in call order, so MergeWith's
// effect is directly observable from the test.
type stubMergeable struct {
	stubToken
	seen []string
}

func (s *stubMergeable) MergeWith(older core.PrimaryExtension) core.PrimaryExtension {
	o, ok := older.(*stubMergeable)
	if !ok {
		return s
	}
	merged := make([]string, 0, len(o.seen)+len(s.seen))
	merged = append(merged, o.seen...)
	merged = append(merged, s.seen...)
	return &stubMergeable{stubToken: s.stubToken, seen: merged}
}

// stubKeyword exercises the keyword-dispatch path so the two maps
// (byToken, byKeyword) are covered independently.
type stubKeyword struct{ tag string }

func (s stubKeyword) Name() string                        { return s.tag }
func (stubKeyword) Keyword() string                       { return "stub_keyword" }
func (stubKeyword) Parse(*core.Parser) (core.Expr, error) { return nil, nil }

type mergeableKeyword struct {
	stubKeyword
	seen []string
}

func (s *mergeableKeyword) MergeWith(older core.PrimaryExtension) core.PrimaryExtension {
	o, ok := older.(*mergeableKeyword)
	if !ok {
		return s
	}
	merged := make([]string, 0, len(o.seen)+len(s.seen))
	merged = append(merged, o.seen...)
	merged = append(merged, s.seen...)
	return &mergeableKeyword{stubKeyword: s.stubKeyword, seen: merged}
}

// ── happy paths ───────────────────────────────────────────────────────

func TestPrimaryTable_SingleToken_Registered(t *testing.T) {
	table := core.NewPrimaryExtensionTable(stubToken{tag: "a"})

	ext, ok := table.ByToken(core.TokAtPrompt)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, ext.Name(), "a")
}

func TestPrimaryTable_SingleKeyword_Registered(t *testing.T) {
	table := core.NewPrimaryExtensionTable(stubKeyword{tag: "a"})

	ext, ok := table.ByKeyword("stub_keyword")
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, ext.Name(), "a")
}

// ── bad paths ─────────────────────────────────────────────────────────

func TestPrimaryTable_NilExtension_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.NewPrimaryExtensionTable(nil)
	})
}

func TestPrimaryTable_EmptyName_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.NewPrimaryExtensionTable(stubToken{tag: ""})
	})
}

func TestPrimaryTable_PlainDuplicateToken_Panics(t *testing.T) {
	// The invariant that predates MergeablePrimary: two primaries with
	// no merge contract cannot claim the same token. If this ever stops
	// panicking, the "duplicates fail loudly" rule has been weakened.
	ktest.RequirePanics(t, func() {
		core.NewPrimaryExtensionTable(
			stubToken{tag: "a"},
			stubToken{tag: "b"},
		)
	})
}

func TestPrimaryTable_PlainDuplicateKeyword_Panics(t *testing.T) {
	ktest.RequirePanics(t, func() {
		core.NewPrimaryExtensionTable(
			stubKeyword{tag: "a"},
			stubKeyword{tag: "b"},
		)
	})
}

// ── merge mechanics ───────────────────────────────────────────────────

func TestPrimaryTable_MergeableToken_Merges(t *testing.T) {
	table := core.NewPrimaryExtensionTable(
		&stubMergeable{stubToken: stubToken{tag: "older"}, seen: []string{"older"}},
		&stubMergeable{stubToken: stubToken{tag: "newer"}, seen: []string{"newer"}},
	)

	ext, ok := table.ByToken(core.TokAtPrompt)
	ktest.RequireTrue(t, ok)

	merged, ok := ext.(*stubMergeable)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, merged.seen, []string{"older", "newer"})
}

func TestPrimaryTable_MergeableKeyword_Merges(t *testing.T) {
	table := core.NewPrimaryExtensionTable(
		&mergeableKeyword{stubKeyword: stubKeyword{tag: "older"}, seen: []string{"older"}},
		&mergeableKeyword{stubKeyword: stubKeyword{tag: "newer"}, seen: []string{"newer"}},
	)

	ext, ok := table.ByKeyword("stub_keyword")
	ktest.RequireTrue(t, ok)

	merged, ok := ext.(*mergeableKeyword)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, merged.seen, []string{"older", "newer"})
}

func TestPrimaryTable_PlainNewerOverMergeableOlder_Panics(t *testing.T) {
	// Order matters. The panic-vs-merge decision is made by the NEWER
	// extension. A mergeable older does not protect a plain newer: the
	// interface is opt-in for the entrant, not a property of the slot.
	ktest.RequirePanics(t, func() {
		core.NewPrimaryExtensionTable(
			&stubMergeable{stubToken: stubToken{tag: "older"}, seen: []string{"older"}},
			stubToken{tag: "newer"},
		)
	})
}

func TestPrimaryTable_ThreeWayMerge_Ordered(t *testing.T) {
	table := core.NewPrimaryExtensionTable(
		&stubMergeable{stubToken: stubToken{tag: "one"}, seen: []string{"one"}},
		&stubMergeable{stubToken: stubToken{tag: "two"}, seen: []string{"two"}},
		&stubMergeable{stubToken: stubToken{tag: "three"}, seen: []string{"three"}},
	)

	ext, ok := table.ByToken(core.TokAtPrompt)
	ktest.RequireTrue(t, ok)

	merged, ok := ext.(*stubMergeable)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, merged.seen, []string{"one", "two", "three"})
}

// ── All() semantics ───────────────────────────────────────────────────

func TestPrimaryTable_All_PreservesInputOrder(t *testing.T) {
	table := core.NewPrimaryExtensionTable(
		stubToken{tag: "a"},
		stubKeyword{tag: "b"},
	)

	all := table.All()
	ktest.RequireLen(t, all, 2)
	ktest.RequireEqual(t, all[0].Name(), "a")
	ktest.RequireEqual(t, all[1].Name(), "b")
}

func TestPrimaryTable_All_HoldsOriginalEntriesAfterMerge(t *testing.T) {
	// Documented property: a merge replaces the map entry but leaves the
	// ordered slice as-is. This is safe because All() is only ever read
	// on the base table (cfg.Primaries), which never contains a mergeable
	// primary — a merge can only occur when the same source contributes
	// its primary twice, which happens one level up, on a fresh table.
	table := core.NewPrimaryExtensionTable(
		&stubMergeable{stubToken: stubToken{tag: "older"}, seen: []string{"older"}},
		&stubMergeable{stubToken: stubToken{tag: "newer"}, seen: []string{"newer"}},
	)

	ktest.RequireLen(t, table.All(), 2)

	ext, ok := table.ByToken(core.TokAtPrompt)
	ktest.RequireTrue(t, ok)
	merged, ok := ext.(*stubMergeable)
	ktest.RequireTrue(t, ok)
	ktest.RequireEqual(t, merged.seen, []string{"older", "newer"})
}
