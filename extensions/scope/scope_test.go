package scope_test

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

// ── @scope behavior ────────────────────────────────────────────────

func TestScope_TimeoutApplies(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :timeout=1ms {
  ` + sleep(200) + `
}`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "scope :timeout must reach the atom")
}

func TestScope_NoTimeoutWithoutScope(t *testing.T) {
	cfg := buildConfig(t)
	_, err := run(t, cfg, sleep(10))
	ktest.RequireNoError(t, err)
}

func TestScope_AtomBeatsScope(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :timeout=1ms {
  sleep:timeout=1s @{ duration_ms: 10 }
}`
	_, err := run(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestScope_InnerBeatsOuter(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :timeout=1ms {
  @scope :timeout=1s {
    ` + sleep(10) + `
  }
}`
	_, err := run(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestScope_NoStacking(t *testing.T) {
	// Outer 1ms would kill the sleep even after inner raises to 1s,
	// if modifiers stacked. Inner-over-outer replacement proves they
	// do not.
	cfg := buildConfig(t)
	src := `@scope :timeout=1ms {
  @scope :timeout=1s {
    ` + sleep(50) + `
  }
}`
	_, err := run(t, cfg, src)
	ktest.RequireNoError(t, err)
}

func TestScope_EmptyBody(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :timeout=1ms {
}
const @{ value: "ok" }`
	ex := mustRun(t, cfg, src)
	ktest.RequireEqual(t, ex.Output, "ok")
}

func TestScope_NoModifiers(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope {
  const @{ value: "ok" }
}`
	ex := mustRun(t, cfg, src)
	ktest.RequireEqual(t, ex.Output, "ok")
}

// ── Scope error paths ──────────────────────────────────────────────

func TestScope_UnknownModifierRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :nope {
  const @{ value: 1 }
}`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected unknown modifier error")
	ktest.RequireStringContains(t, err.Error(), "nope")
}

func TestScope_UnclosedBlockRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :tag="x" {
  const @{ value: 1 }`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected unclosed error")
	ktest.RequireStringContains(t, err.Error(), "unclosed")
}

func TestScope_TrailingContentRejected(t *testing.T) {
	cfg := buildConfig(t)
	src := `@scope :tag="x" { const @{ value: 1 } }`
	_, err := run(t, cfg, src)
	ktest.RequireCondition(t, err != nil, "expected inline-rejected error")
	ktest.RequireStringContains(t, err.Error(), "line after the opening")
}
