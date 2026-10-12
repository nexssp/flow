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
