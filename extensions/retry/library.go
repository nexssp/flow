// Package retry adds retry resilience to individual Flow atoms through
// the AtomAdvise contract. When an atom carries :retry=N, the adviser
// consumes the modifier and installs the retry middleware before the
// modifier table sees the atom.
//
// The `@require` options block can configure the default retry policy:
//
//	@require retry { default_max: "5", backoff: "exponential", retry_all: "false" }
//
// A Go host may set Config.RetryPredicate to supply a typed Kernel retry
// classifier. Predicates are host configuration only; no predicate expression
// is available in NFlow syntax.
//
// Typical use:
//
//	flaky.service:retry=3
package retry

import (
	"embed"
	"fmt"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const ID = "retry"

var acceptedOptions = []string{"default_max", "backoff", "min_backoff", "max_backoff", "retry_all"}

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle satisfies the standard BundleFactory signature, so
// native.Bundles() can mount it the same way as every other bundle.
// Options come from the @require block, or from BundleWithConfig when
// the caller wants to pin the policy in code.
func Bundle(opts map[string]string) compiler.Bundle {
	cfg, configErr := ConfigFromOptions(opts)
	if configErr != nil {
		cfg = Config{}.normalized()
	}
	bundle := BundleWithConfig(cfg)
	bundle.AcceptedOptions = append([]string(nil), acceptedOptions...)
	if configErr != nil {
		bundle.AtomAdvise = func(_ *compiler.Atom, _ *action.Builder[any, any]) error {
			return fmt.Errorf("retry @require options: %w", configErr)
		}
	}
	return bundle
}

// BundleWithConfig returns the retry extension with an explicit policy.
func BundleWithConfig(cfg Config) compiler.Bundle {
	cfg = cfg.normalized()
	configErr := cfg.validate()
	return compiler.Bundle{
		ID:              ID,
		Libraries:       []action.Library{{Name: ID}},
		Build:           compiler.BuildExtension{Modifiers: []string{"retry"}},
		Fixtures:        fixturesFS,
		AcceptedOptions: append([]string(nil), acceptedOptions...),
		AtomAdvise: func(atom *compiler.Atom, builder *action.Builder[any, any]) error {
			if configErr != nil {
				return fmt.Errorf("retry configuration: %w", configErr)
			}
			return advise(cfg, atom, builder)
		},
	}
}
