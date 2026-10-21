package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/runner"
)

type invocation struct {
	host            *runner.Host
	injected        []compiler.Bundle
	targets         map[string]struct{}
	nativeFactory   func() []compiler.Bundle
	native          []compiler.Bundle
	nativeErr       error
	nativeMade      bool
	directives      *compiler.DirectiveTable
	selftest        []compiler.Bundle
	selftestFactory func([]compiler.Bundle) []compiler.Bundle
	selfErr         error
	selfMade        bool
}

func newInvocation(bundles []compiler.Bundle, targets []string) (*invocation, error) {
	host := runner.NewHost()
	injected := append([]compiler.Bundle(nil), bundles...)
	if err := host.OwnAll(injected); err != nil {
		return nil, err
	}
	inv := &invocation{
		host:     host,
		injected: injected,
		targets:  make(map[string]struct{}, len(targets)),
	}
	for _, target := range targets {
		inv.targets[target] = struct{}{}
	}
	return inv, nil
}

func runWithBundleFactories(
	ctx context.Context,
	targets []string,
	construct func(func(compiler.Bundle) error) error,
	dispatch func(*invocation, context.Context) int,
) int {
	inv, err := newNativeInvocation(nil, targets)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(ctx, inv, func(runCtx context.Context) int {
		if construct != nil {
			if err := construct(func(bundle compiler.Bundle) error {
				if err := inv.host.Own(bundle); err != nil {
					return err
				}
				inv.injected = append(inv.injected, bundle)
				return nil
			}); err != nil {
				return fatalf("bundle construction: %v", err)
			}
		}
		return dispatch(inv, runCtx)
	})
}

func runInvocation(ctx context.Context, inv *invocation, dispatch func(context.Context) int) int {
	if inv == nil || inv.host == nil {
		fmt.Fprintln(os.Stderr, "flow host: invocation is unavailable")
		return 1
	}
	if dispatch == nil {
		fmt.Fprintln(os.Stderr, "flow host: invocation callback is nil")
		return 1
	}
	code := 2
	err := inv.host.Run(ctx, func(runCtx context.Context) error {
		code = dispatch(runCtx)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func (inv *invocation) nativeBundles() ([]compiler.Bundle, error) {
	if inv.nativeMade {
		return inv.native, inv.nativeErr
	}
	inv.nativeMade = true
	if inv.nativeFactory == nil {
		inv.nativeErr = xerr.Internal("flow host: native bundle factory is not configured")
		return nil, inv.nativeErr
	}
	inv.native = inv.nativeFactory()
	if err := inv.host.OwnAll(inv.native); err != nil {
		inv.nativeErr = err
		return nil, err
	}
	return inv.native, nil
}

func (inv *invocation) directiveTable() (*compiler.DirectiveTable, error) {
	if inv.directives != nil {
		return inv.directives, nil
	}
	bundles, err := inv.nativeBundles()
	if err != nil {
		return nil, err
	}
	var directives []compiler.Directive
	for i := range bundles {
		directives = append(directives, bundles[i].Directives...)
	}
	inv.directives = compiler.NewDirectiveTable(directives...)
	return inv.directives, nil
}

func (inv *invocation) selftestBundles() ([]compiler.Bundle, error) {
	if inv.selfMade {
		return inv.selftest, inv.selfErr
	}
	inv.selfMade = true
	base, err := inv.nativeBundles()
	if err != nil {
		inv.selfErr = err
		return nil, err
	}
	if inv.selftestFactory == nil {
		inv.selfErr = xerr.Internal("flow host: selftest bundles are not available")
		return nil, inv.selfErr
	}
	inv.selftest = inv.selftestFactory(base)
	extra := inv.selftest[len(base):]
	if err := inv.host.OwnAll(extra); err != nil {
		inv.selfErr = err
		return nil, err
	}
	return inv.selftest, nil
}

func (inv *invocation) buildConfig(reqs []require.Requirement) (runner.Config, error) {
	nativeBundles, err := inv.nativeBundles()
	if err != nil {
		return runner.Config{}, err
	}

	unresolved := make([]require.Requirement, 0, len(reqs))
	for i := range reqs {
		if inv.isAlreadyInjected(reqs[i]) {
			continue
		}
		unresolved = append(unresolved, reqs[i])
	}

	var ownershipErr error
	required, err := require.ResolveBundlesWith(unresolved, func(bundle compiler.Bundle) {
		if ownershipErr == nil {
			ownershipErr = inv.host.Own(bundle)
		}
	})
	if err != nil {
		return runner.Config{}, err
	}
	if ownershipErr != nil {
		return runner.Config{}, ownershipErr
	}

	usesResilience := hasBundleID(required, "resilience") || hasBundleID(inv.injected, "resilience")
	usesLegacyRetry := hasBundleID(required, "retry") || hasBundleID(inv.injected, "retry")
	if usesResilience && usesLegacyRetry {
		return runner.Config{}, xerr.Conflict("@require resilience and @require retry cannot be used together; resilience includes retry policy options")
	}
	baseBundles := nativeBundles
	if usesResilience {
		baseBundles = withoutBundleID(nativeBundles, "retry")
	}
	bundles := append([]compiler.Bundle(nil), baseBundles...)
	bundles = append(bundles, inv.injected...)
	bundles = append(bundles, required...)
	return runner.BuildConfig(dedupeBundleIDs(bundles))
}

func hasBundleID(bundles []compiler.Bundle, id string) bool {
	for i := range bundles {
		if bundles[i].ID == id {
			return true
		}
	}
	return false
}

func withoutBundleID(bundles []compiler.Bundle, id string) []compiler.Bundle {
	out := make([]compiler.Bundle, 0, len(bundles))
	for i := range bundles {
		if bundles[i].ID != id {
			out = append(out, bundles[i])
		}
	}
	return out
}

func (inv *invocation) isAlreadyInjected(r require.Requirement) bool {
	if _, ok := inv.targets[r.Import]; ok {
		return true
	}
	if len(inv.injected) == 0 {
		return false
	}
	targetID := require.NormalizeID(r.Import)
	for i := range inv.injected {
		b := &inv.injected[i]
		if b.ID == targetID || b.ID == r.Import || b.ID == r.Alias {
			return true
		}
		if r.IsLoose && (b.ID == filepath.Base(r.LocalPath) || b.ID == targetID) {
			return true
		}
	}
	return false
}

func (inv *invocation) sourceRequiresFromFile(ctx context.Context, path string) ([]require.Requirement, error) {
	src, err := readSourceFile(path)
	if err != nil {
		return nil, err
	}
	directives, err := inv.directiveTable()
	if err != nil {
		return nil, err
	}
	_, meta, err := compiler.Preprocess(ctx, directives, string(src), path)
	if err != nil {
		return nil, err
	}
	return require.FromMeta(meta), nil
}

func SourceRequires(path string) ([]require.Requirement, error) {
	return sourceRequiresFromFile(path)
}

func buildConfig(reqs []require.Requirement) (runner.Config, error) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return runner.Config{}, err
	}
	var cfg runner.Config
	shutdownErr := inv.host.Run(context.Background(), func(context.Context) error {
		var configErr error
		cfg, configErr = inv.buildConfig(reqs)
		return configErr
	})
	return cfg, shutdownErr
}

func sourceRequiresFromFile(path string) ([]require.Requirement, error) {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return nil, err
	}
	var reqs []require.Requirement
	shutdownErr := inv.host.Run(context.Background(), func(ctx context.Context) error {
		var sourceErr error
		reqs, sourceErr = inv.sourceRequiresFromFile(ctx, path)
		return sourceErr
	})
	return reqs, shutdownErr
}
