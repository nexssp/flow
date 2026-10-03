package require

import (
	"fmt"
	"slices"

	"github.com/nexssp/flow/core"
)

// ResolveBundles resolves in-process requirements to core.Bundle instances.
func ResolveBundles(reqs []Requirement) ([]core.Bundle, error) {
	out := make([]core.Bundle, 0, len(reqs))
	for i := range reqs {
		r := &reqs[i]
		targetID := NormalizeID(r.Import)

		factory, ok := core.Lookup(targetID)
		if !ok {
			factory, ok = core.Lookup(r.Import)
		}
		if !ok {
			return nil, fmt.Errorf("@require %s: bundle %q is not registered in this binary", r.Import, targetID)
		}

		bundle := factory(r.Options)
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

func validateOptions(r *Requirement, bundle core.Bundle) error {
	if bundle.AcceptedOptions == nil {
		return nil
	}
	for k := range r.Options {
		if !slices.Contains(bundle.AcceptedOptions, k) {
			return fmt.Errorf(
				"@require %s: unknown option %q (accepted: %v)",
				r.Import, k, bundle.AcceptedOptions)
		}
	}
	return nil
}
