package resilience

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

type atomPolicy struct {
	remaining     []string
	retries       int
	hasRetry      bool
	timeout       time.Duration
	hasTimeout    bool
	concurrency   int32
	hasConcurrent bool
	breakerKey    string
	hasBreaker    bool
}

func advise(cfg Config, atom *compiler.Atom, builder *action.Builder[any, any]) error {
	if atom == nil || builder == nil {
		return nil
	}
	cfg = cfg.normalized()
	if err := cfg.validate(); err != nil {
		return err
	}
	policy, err := parseAtomPolicy(atom.Modifiers, cfg.Retry.DefaultMax)
	if err != nil {
		return err
	}
	if !policy.applies() {
		return nil
	}
	resilienceConfig, err := policy.kernelConfig(cfg, builder)
	if err != nil {
		return err
	}
	if err := builder.ApplyResilience(resilienceConfig); err != nil {
		return fmt.Errorf("apply resilience: %w", err)
	}
	atom.Modifiers = policy.remaining
	return nil
}

func parseAtomPolicy(modifiers []string, defaultRetries int) (atomPolicy, error) {
	policy := atomPolicy{remaining: make([]string, 0, len(modifiers)), retries: defaultRetries}
	for _, raw := range modifiers {
		name, value := split(raw)
		switch name {
		case "retry":
			policy.hasRetry = true
			if value == "" {
				continue
			}
			retries, err := parseRetryCount(value)
			if err != nil {
				return atomPolicy{}, err
			}
			policy.retries = retries
		case "timeout":
			if policy.hasTimeout {
				return atomPolicy{}, errorsForDuplicate(name)
			}
			timeout, err := parsePositiveDuration(name, value)
			if err != nil {
				return atomPolicy{}, err
			}
			policy.hasTimeout = true
			policy.timeout = timeout
		case "concurrency":
			if policy.hasConcurrent {
				return atomPolicy{}, errorsForDuplicate(name)
			}
			limit, err := parseConcurrency(value)
			if err != nil {
				return atomPolicy{}, err
			}
			policy.hasConcurrent = true
			policy.concurrency = limit
		case "circuit_breaker":
			if policy.hasBreaker {
				return atomPolicy{}, errorsForDuplicate(name)
			}
			policy.hasBreaker = true
			policy.breakerKey = strings.TrimSpace(value)
		default:
			policy.remaining = append(policy.remaining, raw)
		}
	}
	return policy, nil
}

func parseRetryCount(value string) (int, error) {
	retries, err := strconv.Atoi(value)
	if err != nil || retries < 0 {
		return 0, xerr.Validation(fmt.Sprintf("retry expects a non-negative retry count, got %q", value))
	}
	return retries, nil
}

func parsePositiveDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, xerr.Validation(fmt.Sprintf("%s expects a positive duration, got %q", name, value))
	}
	return duration, nil
}

func parseConcurrency(value string) (int32, error) {
	limit, err := strconv.ParseInt(value, 10, 32)
	if err != nil || limit <= 0 {
		return 0, xerr.Validation(fmt.Sprintf("concurrency expects a positive limit, got %q", value))
	}
	return int32(limit), nil
}

func errorsForDuplicate(name string) error {
	return xerr.Validation(fmt.Sprintf("modifier :%s is not repeatable", name))
}

func (policy atomPolicy) applies() bool {
	return policy.hasRetry || policy.hasTimeout || policy.hasConcurrent || policy.hasBreaker
}

func (policy atomPolicy) kernelConfig(cfg Config, builder *action.Builder[any, any]) (action.ResilienceConfig, error) {
	resilienceConfig := action.ResilienceConfig{}
	if policy.hasRetry {
		resilienceConfig.MaxRetries = policy.retries
		resilienceConfig.Backoff = cfg.Retry.BackoffFunc()
		switch {
		case cfg.Retry.RetryAll:
			resilienceConfig.Predicate = action.AlwaysRetryPredicate
		case cfg.Retry.RetryPredicate != nil:
			resilienceConfig.Predicate = cfg.Retry.RetryPredicate
		}
	}
	if policy.hasTimeout {
		resilienceConfig.Timeout = policy.timeout
	}
	if policy.hasConcurrent {
		resilienceConfig.MaxConcurrent = policy.concurrency
	}
	if policy.hasBreaker {
		state, err := breakerState(cfg, policy.breakerKey, builder)
		if err != nil {
			return action.ResilienceConfig{}, err
		}
		resilienceConfig.CircuitBreaker = state
	}
	return resilienceConfig, nil
}

func breakerState(cfg Config, key string, builder *action.Builder[any, any]) (*action.CircuitBreakerState, error) {
	if key == "" {
		meta := builder.Describe()
		if meta == nil || strings.TrimSpace(meta.Name) == "" {
			return nil, errMissingActionName
		}
		key = strings.TrimSpace(meta.Name)
	}
	state, err := cfg.registry().GetOrCreate(key, cfg.CircuitBreaker)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker %q: %w", key, err)
	}
	return state, nil
}

func split(raw string) (name, value string) {
	name, value, _ = strings.Cut(raw, "=")
	return name, value
}
