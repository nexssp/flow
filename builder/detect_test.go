package builder

import "testing"

func TestDetectUsedNativeBundlesIncludesFileLocalSymbols(t *testing.T) {
	source := `@symbols routing {
    needs_review: "NEEDS_REVIEW"
}
route.check`

	used := detectUsedNativeBundles(source, false)
	for _, bundle := range used {
		if bundle.id == "symbols" {
			if bundle.pkg != "github.com/nexssp/flow/extensions/symbols" {
				t.Fatalf("symbols package = %q", bundle.pkg)
			}
			return
		}
	}
	t.Fatal("file-local @symbols directive did not select the native symbols bundle")
}

func TestDetectUsedNativeBundlesIncludesSymbolsForIncludedFlows(t *testing.T) {
	source := `@include "shared/routing.nflow"
route.check`
	used := detectUsedNativeBundles(source, false)
	for _, bundle := range used {
		if bundle.id == "symbols" {
			if bundle.pkg != "github.com/nexssp/flow/extensions/symbols" {
				t.Fatalf("symbols package = %q", bundle.pkg)
			}
			return
		}
	}
	t.Fatal("symbols bundle must be linked because an included flow may declare @symbols")
}

func TestDetectUsedNativeBundlesSelectsResilienceWithoutDuplicateIDs(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		forceAll       bool
		wantResilience bool
		wantRetry      bool
	}{
		{name: "require resilience", source: "@require resilience", wantResilience: true},
		{name: "breaker modifier", source: "payment.charge :circuit_breaker=payments", wantResilience: true},
		{name: "legacy retry only", source: "payment.charge :retry", wantRetry: true},
		{name: "require resilience with retry policy", source: "@require resilience\npayment.charge :retry", wantResilience: true},
		{name: "breaker modifier with retry policy", source: "payment.charge :circuit_breaker=payments :retry", wantResilience: true},
		{name: "legacy retry requirement", source: "@require retry { default_max: \"2\" }\npayment.charge :retry", wantRetry: true},
		{name: "all bundles with resilience", source: "@require resilience\npayment.charge :retry", forceAll: true, wantResilience: true},
		{name: "all bundles with legacy retry", source: "payment.charge :retry", forceAll: true, wantRetry: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			used := detectUsedNativeBundles(test.source, test.forceAll)
			seen := make(map[string]bool, len(used))
			var gotResilience, gotRetry bool
			for _, bundle := range used {
				if seen[bundle.id] {
					t.Fatalf("native bundle %q selected more than once", bundle.id)
				}
				seen[bundle.id] = true
				gotResilience = gotResilience || bundle.id == "resilience"
				gotRetry = gotRetry || bundle.id == "retry"
			}
			if gotResilience != test.wantResilience || gotRetry != test.wantRetry {
				t.Fatalf("selected resilience=%t retry=%t, want resilience=%t retry=%t", gotResilience, gotRetry, test.wantResilience, test.wantRetry)
			}
		})
	}
}
