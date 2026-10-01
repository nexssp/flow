package selftest

import "github.com/nexssp/flow/core"

// Sections returns every feature contributed by every registered bundle.
// Inline self-tests come first (in declaration order); auto-discovered
// fixtures follow in lexical order.
func Sections() []core.SelfTestSection {
	var out []core.SelfTestSection
	bundles := core.RegisteredBundles()
	for i := range bundles {
		out = append(out, bundles[i].AllSelfTests()...)
	}
	return out
}
