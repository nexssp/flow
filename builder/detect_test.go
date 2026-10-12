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
