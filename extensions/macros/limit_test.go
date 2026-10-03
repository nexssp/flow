package macros_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/macros"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func limitsConfig(t *testing.T) runner.Config {
	t.Helper()
	cfg, err := runner.BuildConfig([]core.Bundle{
		syntax.Bundle(nil),
		runtime.Bundle(nil),
		macros.Bundle(nil),
	})
	ktest.RequireNoError(t, err)
	return cfg
}

func limitsRun(t *testing.T, src string) (runner.Execution, error) {
	t.Helper()
	return runner.Execute(context.Background(), limitsConfig(t), src, "test.nflow", nil)
}

// ── Body size ──────────────────────────────────────────────────────

func TestMacro_BodySizeLimit(t *testing.T) {
	big := strings.Repeat("x", 33*1024)
	src := "@macro big() {\n  const @{ value: \"" + big + "\" }\n}\n"

	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected body-size error")
	ktest.RequireStringContains(t, err.Error(), "body is")
	ktest.RequireStringContains(t, err.Error(), "limit")
}

func TestMacro_BodyUnderLimitAccepted(t *testing.T) {
	small := strings.Repeat("x", 1024)
	src := "@macro small() {\n  const @{ value: \"" + small + "\" }\n}\n@small()\n"

	_, err := limitsRun(t, src)
	ktest.RequireNoError(t, err)
}

// ── Recursion ──────────────────────────────────────────────────────

func TestMacro_DirectRecursionRejected(t *testing.T) {
	src := `
@macro loop() {
  @loop()
}

@loop()
`
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "recursion cycle")
	ktest.RequireStringContains(t, err.Error(), "loop -> loop")
}

func TestMacro_SelfRecursionRejected(t *testing.T) {
	src := `
@macro self() {
  noop -> @self()
}

@self()
`
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "recursion cycle")
}

func TestMacro_IndirectRecursionRejected(t *testing.T) {
	src := `
@macro a() {
  @b()
}

@macro b() {
  @c()
}

@macro c() {
  @a()
}

@a()
`
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "recursion cycle")
	// The DFS starts from the newest declaration (c) because that is
	// the one that closes the cycle. The chain is complete and
	// deterministic; which node it starts from depends on declaration
	// order, not on the shape of the cycle.
	ktest.RequireStringContains(t, err.Error(), "c -> a -> b -> c")
}

func TestMacro_MutualRecursionRejected(t *testing.T) {
	src := `
@macro ping() {
  @pong()
}

@macro pong() {
  @ping()
}

@ping()
`
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "recursion cycle")
	ktest.RequireStringContains(t, err.Error(), "pong -> ping -> pong")
}

// ── Expansion depth ────────────────────────────────────────────────

func TestMacro_ChainWithinDepthLimitAccepted(t *testing.T) {
	src := chainSource(10)
	_, err := limitsRun(t, src)
	ktest.RequireNoError(t, err)
}

func TestMacro_ChainAboveDepthLimitRejected(t *testing.T) {
	src := chainSource(20)
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected depth error")
	ktest.RequireStringContains(t, err.Error(), "expansion depth exceeded")
}

// chainSource builds a linear macro chain m0 -> m1 -> ... -> mN where
// mN is a leaf returning "ok". It returns a complete DSL source that
// invokes @m0.
func chainSource(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "@macro m%d() {\n  @m%d()\n}\n\n", i, i+1)
	}
	fmt.Fprintf(&b, "@macro m%d() {\n  const @{ value: \"ok\" }\n}\n\n@m0()\n", n)
	return b.String()
}

// ── String and comment skipping ────────────────────────────────────

func TestMacro_RefInsideStringIsNotAnEdge(t *testing.T) {
	// The body literally contains the text "@loop" but it is inside a
	// string literal; extractMacroRefs must not treat it as a
	// reference, so no cycle is detected and the macro expands.
	src := `
@macro safe() {
  const @{ value: "@loop" }
}

@safe()
`
	ex, err := limitsRun(t, src)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "@loop")
}

func TestMacro_RefInsideCommentIsNotAnEdge(t *testing.T) {
	src := `
@macro safe() {
  # not a real call: @loop
  const @{ value: "ok" }
}

@safe()
`
	ex, err := limitsRun(t, src)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, ex.Output, "ok")
}

func TestMacro_RealRefAmongStringsIsDetected(t *testing.T) {
	// A real cycle where the ref is mixed with string literals that
	// also contain the same text. Only the real ref counts.
	src := `
@macro a() {
  const @{ value: "@b" }
  -> @b()
}

@macro b() {
  const @{ value: "@a" }
  -> @a()
}
`
	_, err := limitsRun(t, src)
	ktest.RequireCondition(t, err != nil, "expected cycle error")
	ktest.RequireStringContains(t, err.Error(), "recursion cycle")
}
