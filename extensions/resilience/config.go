// Package resilience exposes the Kernel resilience policies to Flow atoms.
package resilience

import (
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/retry"
)

// Config is the Go host configuration for the resilience bundle. Retry uses
// retry.Config so legacy defaults and validation remain the single source of
// truth. Registry is host-only and intentionally has no nflow tag.
type Config struct {
	Retry          retry.Config
	CircuitBreaker action.CircuitBreakerConfig
	Registry       *action.CircuitBreakerRegistry
}

type breakerOptions struct {
	FailureThreshold int           `nflow:"failure_threshold"`
	ResetTimeout     time.Duration `nflow:"reset_timeout"`
}

// ConfigFromOptions strictly decodes options accepted by @require resilience.
// Retry defaults and policy validation are delegated to extensions/retry.
func ConfigFromOptions(options map[string]string) (Config, error) {
	retryOptions := make(map[string]string)
	breakerRaw := make(map[string]string)
	retryKeys := make(map[string]struct{})
	for _, key := range retry.OptionNames() {
		retryKeys[key] = struct{}{}
	}
	for key, value := range options {
		if _, ok := retryKeys[key]; ok {
			retryOptions[key] = value
			continue
		}
		switch key {
		case "failure_threshold", "reset_timeout":
			breakerRaw[key] = value
		default:
			return Config{}, xerr.Validation(fmt.Sprintf("resilience options: unknown option %q", key))
		}
	}

	retryConfig, err := retry.ConfigFromOptions(retryOptions)
	if err != nil {
		return Config{}, xerr.Validation("resilience retry options are invalid", err)
	}
	breaker, err := compiler.Decode[breakerOptions](breakerRaw)
	if err != nil {
		return Config{}, xerr.Validation("resilience options are invalid", err)
	}
	if raw, ok := breakerRaw["failure_threshold"]; ok && raw != "" && breaker.FailureThreshold <= 0 {
		return Config{}, xerr.Validation("resilience: failure_threshold must be positive")
	}
	if raw, ok := breakerRaw["reset_timeout"]; ok && raw != "" && breaker.ResetTimeout <= 0 {
		return Config{}, xerr.Validation("resilience: reset_timeout must be positive")
	}
	cfg := Config{
		Retry: retryConfig,
		CircuitBreaker: action.CircuitBreakerConfig{
			FailureThreshold: breaker.FailureThreshold,
			ResetTimeout:     breaker.ResetTimeout,
		},
	}
	cfg.CircuitBreaker = cfg.CircuitBreaker.Normalized()
	if err := cfg.CircuitBreaker.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// normalized fills in the defaults that are also used by direct host config.
func (cfg Config) normalized() Config {
	cfg.Retry = cfg.Retry.Normalized()
	cfg.CircuitBreaker = cfg.CircuitBreaker.Normalized()
	return cfg
}

func (cfg Config) validate() error {
	cfg = cfg.normalized()
	if err := cfg.Retry.Validate(); err != nil {
		return xerr.Validation("resilience retry configuration is invalid", err)
	}
	return cfg.CircuitBreaker.Validate()
}

func (cfg Config) registry() *action.CircuitBreakerRegistry {
	if cfg.Registry != nil {
		return cfg.Registry
	}
	return action.NewCircuitBreakerRegistry()
}

var errMissingActionName = xerr.Validation("resilience: circuit breaker requires a non-empty action name when no key is supplied")
