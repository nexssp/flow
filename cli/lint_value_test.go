package cli

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
)

// lintFixture builds the (cfg, known, modifiers) triple that lintFile
// passes to lintFragment. It uses a real BuildConfig — native bundles
// included — so the modifier table under test is the one production
// installs, not a hand-built substitute that could drift.
//
// primaries is cfg.Primaries (the base table). The tests in this file
// exercise modifier-value validation, not per-source primaries, so the
// base table is the right argument. A test that needed a per-source
// primary would call cfg.PrimariesFor(meta) instead.
func lintFixture(t *testing.T) (
	cfg runner.Config,
	primaries *core.PrimaryExtensionTable,
	known map[string]struct{},
	modifiers map[string]core.Modifier,
) {
	t.Helper()

	var err error
	cfg, err = buildConfig(nil)
	ktest.RequireNoError(t, err)

	primaries = cfg.Primaries

	atoms, mods := registrySurface(cfg)
	known = atoms
	modifiers = mods

	return cfg, primaries, known, modifiers
}

// lintOne runs lintFragment against a single source fragment with the
// production modifier table. The lineMods argument is nil: this file
// tests value validation, not inherited policy.
func lintOne(t *testing.T, src string) []LintIssue {
	t.Helper()
	cfg, primaries, known, modifiers := lintFixture(t)
	return lintFragment("test.nflow", src, cfg, primaries, known, modifiers, nil)
}

// ── valid values ─────────────────────────────────────────────────────

func TestLintFragment_ValidDuration(t *testing.T) {
	issues := lintOne(t, `runtime.noop:timeout=5s`)
	ktest.RequireLen(t, issues, 0)
}

func TestLintFragment_ValidInt(t *testing.T) {
	issues := lintOne(t, `runtime.noop:retry=3`)
	ktest.RequireLen(t, issues, 0)
}

func TestLintFragment_ValidFlag(t *testing.T) {
	issues := lintOne(t, `runtime.noop:dedup`)
	ktest.RequireLen(t, issues, 0)
}

func TestLintFragment_ValidStringList(t *testing.T) {
	issues := lintOne(t, `runtime.noop:role=admin,editor`)
	ktest.RequireLen(t, issues, 0)
}

// ── invalid values ───────────────────────────────────────────────────

func TestLintFragment_InvalidDuration(t *testing.T) {
	issues := lintOne(t, `runtime.noop:timeout=notaduration`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "invalid_modifier_value")
	ktest.RequireStringContains(t, issues[0].Message, "expected duration")
}

func TestLintFragment_InvalidInt(t *testing.T) {
	issues := lintOne(t, `runtime.noop:retry=abc`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "invalid_modifier_value")
	ktest.RequireStringContains(t, issues[0].Message, "expected integer")
}

func TestLintFragment_FlagWithValue(t *testing.T) {
	// :dedup is a flag; :dedup=yes is a mistake the author should see.
	issues := lintOne(t, `runtime.noop:dedup=yes`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "invalid_modifier_value")
	ktest.RequireStringContains(t, issues[0].Message, "flag does not take a value")
}

// ── unknown modifiers and atoms ──────────────────────────────────────

func TestLintFragment_UnknownModifier(t *testing.T) {
	issues := lintOne(t, `runtime.noop:timetout=5s`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "unknown_modifier")
}

func TestLintFragment_UnknownAtom(t *testing.T) {
	issues := lintOne(t, `does.not.exist`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "unknown_atom")
}

// ── parse failures ───────────────────────────────────────────────────

func TestLintFragment_ParseError(t *testing.T) {
	issues := lintOne(t, `runtime.noop -> )`)
	ktest.RequireLen(t, issues, 1)
	ktest.RequireEqual(t, issues[0].Kind, "parse")
}
