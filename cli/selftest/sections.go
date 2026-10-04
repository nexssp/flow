package selftest

import (
	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
)

// Sections returns every feature contributed by every bundle the
// self-test environment loads. Inline self-tests come first (in bundle
// declaration order); auto-discovered fixtures follow in lexical order
// within each bundle.
//
// The bundle set is native.SelftestBundles() — the same set Run()
// builds its config from. Using one source for both ensures the
// features listed are exactly the features that will be executed, and
// that no feature can be reported from a bundle the runner does not
// have.
func Sections() []core.SelfTestSection {
	var out []core.SelfTestSection
	bundles := native.SelftestBundles()
	for i := range bundles {
		out = append(out, bundles[i].AllSelfTests()...)
	}
	return out
}
