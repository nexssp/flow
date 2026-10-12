package require

import (
	"fmt"
	"slices"

	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

// ResolveBundles resolves in-process requirements to compiler.Bundle instances.
func ResolveBundles(reqs []Requirement) ([]compiler.Bundle, error) {
	return ResolveBundlesWith(reqs, nil)
}

// ResolveBundlesWith resolves in-process requirements and calls onBundle
// immediately after each factory returns, before option validation or alias
// wiring. Owners can therefore retain cleanup callbacks when a later bundle
// fails validation or construction.
func ResolveBundlesWith(reqs []Requirement, onBundle func(compiler.Bundle)) ([]compiler.Bundle, error) {
	out := make([]compiler.Bundle, 0, len(reqs))
	for i := range reqs {
		r := &reqs[i]
		targetID := NormalizeID(r.Import)

		factory, ok := compiler.Lookup(targetID)
		if !ok {
			factory, ok = compiler.Lookup(r.Import)
		}
		if !ok {
			return nil, xerr.NotFound(fmt.Sprintf(
				"@require %s: bundle %q is not registered in this binary",
				r.Import, targetID))
		}

		bundle := factory(r.Options)
		if onBundle != nil {
			onBundle(bundle)
		}
		if err := validateOptions(r, bundle); err != nil {
			return nil, err
		}
		if r.Alias != "" {
			bundle.Alias = r.Alias
		}
		out = append(out, bundle)
	}
	return out, nil
}

func validateOptions(r *Requirement, bundle compiler.Bundle) error {
	if bundle.AcceptedOptions == nil {
		return nil
	}
	for k := range r.Options {
		if !slices.Contains(bundle.AcceptedOptions, k) {
			return xerr.BadRequest(fmt.Sprintf(
				"@require %s: unknown option %q (accepted: %v)",
				r.Import, k, bundle.AcceptedOptions))
		}
	}
	return nil
}
