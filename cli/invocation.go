package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/runner"
)

type invocation struct {
	host             *runner.Host
	toolchainEnabled bool
	injected         []compiler.Bundle
	modeBundles      []compiler.Bundle
	targets          map[string]struct{}
	nativeFactory    func() []compiler.Bundle
	native           []compiler.Bundle
	nativeErr        error
	nativeMade       bool
	directives       *compiler.DirectiveTable
	selftest         []compiler.Bundle
	selftestFactory  func([]compiler.Bundle) []compiler.Bundle
	selfErr          error
	selfMade         bool
	required         map[string]compiler.Bundle
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
	if inv.directives != nil && len(inv.modeBundles) == 0 {
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
	for i := range inv.modeBundles {
		directives = append(directives, inv.modeBundles[i].Directives...)
	}
	table := compiler.NewDirectiveTable(directives...)
	if len(inv.modeBundles) == 0 {
		inv.directives = table
	}
	return table, nil
}

func (inv *invocation) requirementDiscoveryTable() (*compiler.DirectiveTable, error) {
	bundles, err := inv.nativeBundles()
	if err != nil {
		return nil, err
	}
	var directives []compiler.Directive
	for i := range bundles {
		for _, directive := range bundles[i].Directives {
			if directive.Name != "include" {
				directives = append(directives, directive)
			}
		}
	}
	for i := range inv.modeBundles {
		for _, directive := range inv.modeBundles[i].Directives {
			if directive.Name != "include" {
				directives = append(directives, directive)
			}
		}
	}
	return compiler.NewDirectiveTable(directives...), nil
}

func (inv *invocation) discoverRootRequirements(ctx context.Context, source, name string) ([]require.Requirement, error) {
	discovery, err := inv.requirementDiscoveryTable()
	if err != nil {
		return nil, err
	}
	_, meta, err := compiler.Preprocess(ctx, discovery, source, name)
	if err != nil {
		return nil, err
	}
	return require.SourceRequirements(meta)
}

// requirementsForHarness preserves metadata-only discovery for ordinary files,
// then falls back to a bundle-aware pass for sources importing bundle modules.
func (inv *invocation) requirementsForHarness(ctx context.Context, path string) ([]require.Requirement, error) {
	requirements, err := inv.sourceRequiresFromFile(ctx, path)
	if err == nil {
		return requirements, nil
	}
	source, readErr := readSourceFile(path)
	if readErr != nil {
		return nil, readErr
	}
	rootRequirements, discoveryErr := inv.discoverRootRequirements(ctx, string(source), path)
	if discoveryErr != nil {
		return nil, err
	}
	if requirementsNeedExternalHarness(inv, rootRequirements) {
		return rootRequirements, nil
	}
	cfg, configErr := inv.buildConfig(rootRequirements)
	if configErr != nil {
		return nil, configErr
	}
	_, meta, preprocessErr := compiler.PreprocessWithOptions(ctx, cfg.Directives, string(source), path, cfg.CompileOpts...)
	if preprocessErr != nil {
		return nil, preprocessErr
	}
	return require.SourceRequirements(meta)
}

// prepareRequiredSource first discovers root requirements, then preprocesses
// with those bundles available so @include can load exported bundle modules.
// It repeats only when a local include contributes additional requirements.
func (inv *invocation) prepareRequiredSource(ctx context.Context, source, name string) (cleanSource string, metadata map[string]any, config runner.Config, err error) {
	return inv.prepareRequiredSourceData(ctx, source, name)
}

func (inv *invocation) prepareRequiredPreparedSource(ctx context.Context, source, name string) (*compiler.PreparedSource, runner.Config, error) {
	clean, metadata, cfg, err := inv.prepareRequiredSourceData(ctx, source, name)
	if err != nil {
		return nil, runner.Config{}, err
	}
	prepared := compiler.PreparedSourceFromPreprocessed(source, name, clean, metadata, cfg.CompileOpts...)
	return prepared, cfg, nil
}

func (inv *invocation) prepareRequiredSourceData(ctx context.Context, source, name string) (cleanSource string, metadata map[string]any, config runner.Config, err error) {
	requirements, err := inv.discoverRootRequirements(ctx, source, name)
	if err != nil {
		return "", nil, runner.Config{}, err
	}
	for range 16 {
		cfg, configErr := inv.buildConfig(requirements)
		if configErr != nil {
			return "", nil, runner.Config{}, configErr
		}
		clean, meta, preprocessErr := compiler.PreprocessWithOptions(ctx, cfg.Directives, source, name, cfg.CompileOpts...)
		if preprocessErr != nil {
			return "", nil, runner.Config{}, preprocessErr
		}
		allRequirements, requirementsErr := require.SourceRequirements(meta)
		if requirementsErr != nil {
			return "", nil, runner.Config{}, requirementsErr
		}
		if sameSourceRequirements(requirements, allRequirements) {
			return clean, meta, cfg, nil
		}
		requirements = allRequirements
	}
	return "", nil, runner.Config{}, errors.New("source requirements did not stabilize after 16 compile-time passes")
}

func sameSourceRequirements(left, right []require.Requirement) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if sourceRequirementKey(left[i]) != sourceRequirementKey(right[i]) {
			return false
		}
	}
	return true
}

func sourceRequirementKey(r require.Requirement) string {
	var key strings.Builder
	fmt.Fprintf(&key, "%q\x00%q\x00%q", r.Import, r.Version, r.Alias)
	optionNames := make([]string, 0, len(r.Options))
	for name := range r.Options {
		optionNames = append(optionNames, name)
	}
	sort.Strings(optionNames)
	for _, name := range optionNames {
		fmt.Fprintf(&key, "\x00%q=%q", name, r.Options[name])
	}
	return key.String()
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
		if inv.isAlreadyInjected(reqs[i]) || inv.hasRequired(reqs[i]) {
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
	if inv.required == nil {
		inv.required = make(map[string]compiler.Bundle, len(reqs))
	}
	for i := range required {
		inv.required[sourceRequirementKey(unresolved[i])] = required[i]
	}
	required = required[:0]
	for i := range reqs {
		if inv.isAlreadyInjected(reqs[i]) {
			continue
		}
		if bundle, ok := inv.required[sourceRequirementKey(reqs[i])]; ok {
			required = append(required, bundle)
		}
	}

	usesResilience := hasBundleID(required, "resilience") || hasBundleID(inv.injected, "resilience")
	usesLegacyRetry := hasBundleID(required, "retry") || hasBundleID(inv.injected, "retry")
	if usesResilience && usesLegacyRetry {
		return runner.Config{}, xerr.Conflict("@require resilience and @require retry cannot be used together; resilience includes retry policy options")
	}
	baseBundles := nativeBundles
	// An explicit retry requirement configures the native retry policy. This is
	// the only supported same-ID replacement; other duplicates must be errors.
	if usesResilience || hasBundleID(required, "retry") {
		baseBundles = withoutBundleID(nativeBundles, "retry")
	}
	bundles := append([]compiler.Bundle(nil), baseBundles...)
	bundles = append(bundles, inv.injected...)
	bundles = append(bundles, inv.modeBundles...)
	bundles = append(bundles, required...)
	return runner.BuildConfig(bundles)
}

func (inv *invocation) hasRequired(r require.Requirement) bool {
	if inv.required == nil {
		return false
	}
	_, ok := inv.required[sourceRequirementKey(r)]
	return ok
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
	return require.SourceRequirements(meta)
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
