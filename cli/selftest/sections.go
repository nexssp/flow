package selftest

import (
	"context"
	"log/slog"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/native"
	flowrunner "github.com/nexssp/flow/runner"
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
	bundles := native.SelftestBundles()
	host := flowrunner.NewHost()
	if err := host.OwnAll(bundles); err != nil {
		slog.Error("self-test section host setup failed", "error", err)
		return nil
	}
	var sections []core.SelfTestSection
	if err := host.Run(context.Background(), func(context.Context) error {
		sections = SectionsFromBundles(bundles)
		return nil
	}); err != nil {
		slog.Error("self-test section shutdown failed", "error", err)
	}
	return sections
}

// SectionsFromBundles collects features from the supplied already-constructed
// bundle instances, allowing an invocation host to own them through the run.
func SectionsFromBundles(bundles []core.Bundle) []core.SelfTestSection {
	var out []core.SelfTestSection
	for i := range bundles {
		out = append(out, bundles[i].AllSelfTests()...)
	}
	return out
}
