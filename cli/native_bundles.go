package cli

import (
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
)

func newNativeInvocation(bundles []compiler.Bundle, targets []string) (*invocation, error) {
	inv, err := newInvocation(bundles, targets)
	if err != nil {
		return nil, err
	}
	withNativeDefaults(inv)
	return inv, nil
}

func withNativeDefaults(inv *invocation) {
	if inv == nil {
		return
	}
	inv.nativeFactory = native.Bundles
	inv.selftestFactory = native.SelftestBundlesFrom
}
