package scope

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
)

func TestProfileMultilinePreprocess(t *testing.T) {
	t.Parallel()
	source := `@profile base_resilience
  :timeout=4s
  :retry=2
  :concurrency=8

# child policy
@profile hardened_resilience
  :parent=base_resilience

  # override, then flags
  :timeout=2s
  :rate_limit=100 // capacity
  :cache=30s
  :idempotent
  :dedup
  :coalesce

runtime.const
  :timeout=1s
`
	clean, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(ProfileDirective), source, "profiles.nflow")
	ktest.RequireNoError(t, err)
	mods, err := ExpandProfile(meta, "hardened_resilience")
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, mods, []string{"timeout=2s", "retry=2", "concurrency=8", "rate_limit=100", "cache=30s", "idempotent", "dedup", "coalesce"})
	ktest.RequireCondition(t, strings.Contains(clean, "runtime.const\n  :timeout=1s"), "body modifier was consumed: %s", clean)
	ktest.RequireEqual(t, strings.Count(clean, "\n"), strings.Count(source, "\n"))
}

func TestProfileMultilineParentErrors(t *testing.T) {
	t.Parallel()
	for _, tail := range []string{":parent", ":parent=missing :parent=other"} {
		_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(ProfileDirective), "@profile p\n  "+tail, "parent.nflow")
		ktest.RequireErrorContains(t, err, ":parent")
	}
}
