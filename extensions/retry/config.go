package retry

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"

	compiler "github.com/nexssp/flow/compiler"
)

const (
	BackoffConstant          = "constant"
	BackoffExponential       = "exponential"
	BackoffExponentialJitter = "exponential_jitter"
)

// Config controls the retry policy installed by the adviser.
type Config struct {
	DefaultMax int           `nflow:"default_max" default:"3"`
	Backoff    string        `nflow:"backoff" default:"exponential_jitter"`
	MinBackoff time.Duration `nflow:"min_backoff" default:"10ms"`
	MaxBackoff time.Duration `nflow:"max_backoff" default:"2s"`
	RetryAll   bool          `nflow:"retry_all" default:"false"`
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

func (c Config) validate() error {
	if c.DefaultMax <= 0 {
		return errors.New("retry: default_max must be positive")
	}
	if c.MinBackoff <= 0 {
		return errors.New("retry: min_backoff must be positive")
	}
	if c.MaxBackoff <= 0 {
		return errors.New("retry: max_backoff must be positive")
	}
	if c.MaxBackoff < c.MinBackoff {
		return errors.New("retry: max_backoff must be at least min_backoff")
	}
	if c.RetryAll && c.RetryPredicate != nil {
		return errors.New("retry: retry_predicate cannot be combined with retry_all")
	}
	switch c.Backoff {
	case BackoffConstant, BackoffExponential, BackoffExponentialJitter:
		return nil
	default:
		return fmt.Errorf("retry: backoff must be %q, %q, or %q", BackoffConstant, BackoffExponential, BackoffExponentialJitter)
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
		return Config{}, fmt.Errorf("retry options: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg.normalized(), nil
}
