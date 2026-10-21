package resilience

import (
	"embed"
	"fmt"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/retry"
)

const ID = "resilience"

var acceptedOptions = append(retry.OptionNames(), "failure_threshold", "reset_timeout")

//go:embed nflows
var fixturesFS embed.FS

func init() {
	compiler.Register(ID, Bundle)
}

// Bundle builds an instance-owned resilience bundle from @require options.
// Its registry is local to this bundle instance and shared by all compiled
// actions using that instance; it is neither global nor distributed.
func Bundle(options map[string]string) compiler.Bundle {
	cfg, configErr := ConfigFromOptions(options)
	if configErr != nil {
		cfg = Config{}
	}
	bundle := BundleWithConfig(cfg)
	bundle.AcceptedOptions = append([]string(nil), acceptedOptions...)
	if configErr != nil {
		bundle.AtomAdvise = func(*compiler.Atom, *action.Builder[any, any]) error {
			return fmt.Errorf("resilience @require options: %w", configErr)
		}
	}
	return bundle
}

// BundleWithConfig creates a resilience bundle using the supplied host config.
// If Config.Registry is nil, a private registry is allocated for this bundle.
func BundleWithConfig(cfg Config) compiler.Bundle {
	return bundleWithConfig(cfg, nil)
}

// BundleWithRegistry lets a host explicitly share breaker state across bundle
// instances or separately compiled programs. The registry remains process-local.
func BundleWithRegistry(cfg Config, registry *action.CircuitBreakerRegistry) compiler.Bundle {
	return bundleWithConfig(cfg, registry)
}

func bundleWithConfig(cfg Config, registry *action.CircuitBreakerRegistry) compiler.Bundle {
	cfg = cfg.normalized()
	if registry != nil {
		cfg.Registry = registry
	}
	if cfg.Registry == nil {
		cfg.Registry = action.NewCircuitBreakerRegistry()
	}
	configErr := cfg.validate()
	return compiler.Bundle{
		ID:              ID,
		Libraries:       []action.Library{{Name: ID}},
		Build:           compiler.BuildExtension{Modifiers: []string{"retry", "circuit_breaker"}, Replaces: []string{retry.ID}},
		Fixtures:        fixturesFS,
		Modifiers:       Modifiers(),
		AcceptedOptions: append([]string(nil), acceptedOptions...),
		AtomAdvise: func(atom *compiler.Atom, builder *action.Builder[any, any]) error {
			if configErr != nil {
				return fmt.Errorf("resilience configuration: %w", configErr)
			}
			return advise(cfg, atom, builder)
		},
	}
}
