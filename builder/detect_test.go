package builder

import (
	"path/filepath"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func planSourceFixture(t *testing.T, source string, all bool) []nativeBundleInfo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.nflow")
	sources, err := buildSourcesFrom(t.Context(), path, source)
	ktest.RequireNoError(t, err)
	plan, err := nativeBundlesForSources(sources, nil, all)
	ktest.RequireNoError(t, err)
	return plan
}

func planHasBundle(plan []nativeBundleInfo, id string) bool {
	for _, bundle := range plan {
		if bundle.id == id {
			return true
		}
	}
	return false
}

func TestNativePlanUsesFileLocalSymbols(t *testing.T) {
	t.Parallel()
	plan := planSourceFixture(t, "@symbols routing {\nneeds_review: \"NEEDS_REVIEW\"\n}\nroute.check", false)
	ktest.RequireTrue(t, planHasBundle(plan, "symbols"))
	ktest.RequireFalse(t, planHasBundle(planSourceFixture(t, "runtime.noop", false), "symbols"))
}

func TestNativePlanUsesIncludedSymbolsInsteadOfAlwaysIncludingThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	flow := filepath.Join(root, "main.nflow")
	writeSourceGraphFixture(t, flow, "@include child.nflow\nroute.check")
	writeSourceGraphFixture(t, filepath.Join(root, "child.nflow"), "@symbols routing {\nvalue: \"READY\"\n}\n")
	sources, err := buildSources(t.Context(), flow)
	ktest.RequireNoError(t, err)
	plan, err := nativeBundlesForSources(sources, nil, false)
	ktest.RequireNoError(t, err)
	ktest.RequireTrue(t, planHasBundle(plan, "symbols"))
}

func TestNativePlanUsesExtensionOwnedRetryReplacement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		source     string
		all        bool
		resilience bool
		retry      bool
	}{
		{name: "explicit resilience", source: "@require resilience\npayment.charge :retry", resilience: true},
		{name: "legacy retry", source: "payment.charge :retry", retry: true},
		{name: "explicit legacy retry", source: "@require retry { default_max: \"2\" }\npayment.charge :retry", retry: true},
		{name: "all with resilience", source: "@require resilience\npayment.charge :retry", all: true, resilience: true},
		{name: "all standard only", source: "payment.charge :retry", all: true, retry: true},
		{name: "optional not inferred from modifier", source: "payment.charge :circuit_breaker=payments"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := planSourceFixture(t, test.source, test.all)
			ktest.RequireEqual(t, planHasBundle(plan, "resilience"), test.resilience)
			ktest.RequireEqual(t, planHasBundle(plan, "retry"), test.retry)
			seen := make(map[string]bool, len(plan))
			for _, entry := range plan {
				ktest.RequireFalse(t, seen[entry.id])
				seen[entry.id] = true
			}
		})
	}
}

func TestNativePlanIgnoresFeatureNamesInCommentsAndStrings(t *testing.T) {
	t.Parallel()
	plan := planSourceFixture(t, "# fs.read cov.echo render.markdown :retry\n{value: \"log.info @symbols :retry cov.echo\"} -> runtime.noop", false)
	for _, id := range []string{"fs", "nodes_log", "symbols", "retry", "selftestkit", "render"} {
		ktest.RequireFalse(t, planHasBundle(plan, id))
	}
}
