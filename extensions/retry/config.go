package retry

import (
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	compiler "github.com/nexssp/flow/compiler"
)

const (
	BackoffConstant          = "constant"
	BackoffExponential       = "exponential"
	BackoffExponentialJitter = "exponential_jitter"
)

// Config controls the retry policy installed by the adviser.
type Config struct {
	DefaultMax int           `nflow:"default_max"`
	Backoff    string        `nflow:"backoff"`
	MinBackoff time.Duration `nflow:"min_backoff"`
	MaxBackoff time.Duration `nflow:"max_backoff"`
	RetryAll   bool          `nflow:"retry_all"`
	// RetryPredicate is a host-supplied error classifier used by Kernel's
	// Builder.RetryIf. It is deliberately not an @require option and cannot be
	// combined with RetryAll. It may be called concurrently by action runs, so
	// captured mutable state must be synchronized by the host.
	RetryPredicate action.RetryPredicate
}

// normalized fills omitted fields with the existing retry defaults. It does
// not repair invalid non-zero values; those are reported by validate.
func (c Config) normalized() Config {
	if c.DefaultMax == 0 {
		c.DefaultMax = 3
	}
	if c.Backoff == "" {
		c.Backoff = BackoffExponentialJitter
	}
	if c.MinBackoff == 0 {
		c.MinBackoff = 10 * time.Millisecond
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 2 * time.Second
	}
	return c
}

// Normalized returns the existing retry defaults for omitted settings.
func (c Config) Normalized() Config { return c.normalized() }

// Validate checks a fully populated retry configuration.
func (c Config) Validate() error { return c.validate() }

// BackoffFunc creates the configured Kernel retry delay function.
func (c Config) BackoffFunc() func(attempt int) time.Duration { return c.backoffFunc() }

// OptionNames returns the recognized @require option keys.
func OptionNames() []string { return append([]string(nil), acceptedOptions...) }

func (c Config) validate() error {
	if c.DefaultMax <= 0 {
		return xerr.Validation("retry: default_max must be positive")
	}
	if c.MinBackoff <= 0 {
		return xerr.Validation("retry: min_backoff must be positive")
	}
	if c.MaxBackoff <= 0 {
		return xerr.Validation("retry: max_backoff must be positive")
	}
	if c.MaxBackoff < c.MinBackoff {
		return xerr.Validation("retry: max_backoff must be at least min_backoff")
	}
	if c.RetryAll && c.RetryPredicate != nil {
		return xerr.Validation("retry: retry_predicate cannot be combined with retry_all")
	}
	switch c.Backoff {
	case BackoffConstant, BackoffExponential, BackoffExponentialJitter:
		return nil
	default:
		return xerr.Validation(fmt.Sprintf("retry: backoff must be %q, %q, or %q", BackoffConstant, BackoffExponential, BackoffExponentialJitter))
	}
}

func (c Config) backoffFunc() func(attempt int) time.Duration {
	switch c.Backoff {
	case BackoffConstant:
		return action.ConstantBackoff(c.MinBackoff)
	case BackoffExponential:
		return action.ExponentialBackoff(c.MinBackoff, c.MaxBackoff)
	default:
		return action.ExponentialJitter(c.MinBackoff, c.MaxBackoff)
	}
}

// ConfigFromOptions strictly decodes a `@require retry { ... }` options map.
// Missing values use the same defaults as Config.normalized; malformed,
// unknown, or out-of-range values are returned as errors.
func ConfigFromOptions(opts map[string]string) (Config, error) {
	cfg, err := compiler.Decode[Config](opts)
	if err != nil {
		return Config{}, xerr.Validation("retry options are invalid", err)
	}
	if raw, ok := opts["default_max"]; ok && raw != "" && cfg.DefaultMax <= 0 {
		return Config{}, xerr.Validation("retry: default_max must be positive")
	}
	if raw, ok := opts["min_backoff"]; ok && raw != "" && cfg.MinBackoff <= 0 {
		return Config{}, xerr.Validation("retry: min_backoff must be positive")
	}
	if raw, ok := opts["max_backoff"]; ok && raw != "" && cfg.MaxBackoff <= 0 {
		return Config{}, xerr.Validation("retry: max_backoff must be positive")
	}
	cfg = cfg.normalized()
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
