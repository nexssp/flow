// Package retry adds retry resilience to individual Flow atoms through
// the AtomAdvise contract. When an atom carries :retry=N, the adviser
// consumes the modifier and installs the retry middleware before the
// modifier table sees the atom.
//
// The `@require` options block can override the policy defaults:
//
//	@require retry { default_max: "5" }
//
// Typical use:
//
//	flaky.service:retry=3
package retry

import (
	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
)

const ID = "retry"

func init() {
	core.Register(ID, Bundle)
}

// Bundle satisfies the standard BundleFactory signature, so
// native.Bundles() can mount it the same way as every other bundle.
// Options come from the @require block, or from BundleWithConfig when
// the caller wants to pin the policy in code.
func Bundle(opts map[string]string) core.Bundle {
	return BundleWithConfig(ConfigFromOptions(opts))
}

// BundleWithConfig returns the retry extension with an explicit policy.
func BundleWithConfig(cfg Config) core.Bundle {
	cfg = cfg.normalized()
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{{Name: ID}},
		AtomAdvise: func(atom *core.Atom, builder *action.Builder[any, any]) error {
			return advise(cfg, atom, builder)
		},
	}
}
