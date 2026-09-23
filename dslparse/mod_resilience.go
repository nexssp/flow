package dslparse

import (
	"strconv"
	"strings"
	"time"
)

// Resilience modifiers.
//
//	:timeout=, :retry=, :retry_if=, :backoff=, :backoff_base=, :backoff_max=
//	:breaker=, :breaker_failures=, :breaker_cooldown=, :priority=
//	:concurrency=, :rate_limit=, :burst=
func init() {
	registerMod("timeout", func(p *Modifiers, v string, _ bool) error {
		d, err := time.ParseDuration(trimValue(v))
		if err != nil {
			return err
		}
		p.Timeout = d
		return nil
	})

	registerMod("retry", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.Atoi(trimValue(v))
		if err != nil {
			return err
		}
		p.RetryMax = n
		return nil
	})

	registerMod("retry_if", func(p *Modifiers, v string, _ bool) error {
		p.RetryPredicate = trimValue(v)
		return nil
	})

	registerMod("backoff", func(p *Modifiers, v string, _ bool) error {
		applyBackoffSpec(p, trimValue(v))
		return nil
	})

	registerMod("backoff_base", func(p *Modifiers, v string, _ bool) error {
		d, err := time.ParseDuration(trimValue(v))
		if err != nil {
			return err
		}
		p.BackoffBase = d
		return nil
	})

	registerMod("backoff_max", func(p *Modifiers, v string, _ bool) error {
		d, err := time.ParseDuration(trimValue(v))
		if err != nil {
			return err
		}
		p.BackoffMax = d
		return nil
	})

	registerMod("breaker", func(p *Modifiers, v string, _ bool) error {
		applyBreakerSpec(p, trimValue(v))
		return nil
	})

	registerMod("breaker_failures", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.Atoi(trimValue(v))
		if err != nil {
			return err
		}
		p.BreakerFailures = n
		return nil
	})

	registerMod("breaker_cooldown", func(p *Modifiers, v string, _ bool) error {
		d, err := time.ParseDuration(trimValue(v))
		if err != nil {
			return err
		}
		p.BreakerCooldown = d
		return nil
	})

	registerMod("priority", func(p *Modifiers, v string, _ bool) error {
		p.Priority = toLower(trimValue(v))
		return nil
	})

	registerMod("concurrency", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.ParseInt(trimValue(v), 10, 32)
		if err != nil {
			return err
		}
		p.ConcurrencyLimit = int32(n)
		return nil
	})

	registerMod("rate_limit", func(p *Modifiers, v string, _ bool) error {
		val := trimValue(v)
		p.RateLimit = val
		p.RateLimitRPS = parseRPS(val)
		return nil
	})

	registerMod("burst", func(p *Modifiers, v string, _ bool) error {
		n, err := strconv.Atoi(trimValue(v))
		if err != nil {
			return err
		}
		p.RateLimitBurst = n
		return nil
	})
}

func applyBackoffSpec(p *Modifiers, spec string) {
	if spec == "" {
		return
	}
	parts := splitCSV(spec)
	if len(parts) == 0 {
		return
	}
	p.BackoffStrategy = parts[0]
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch k {
		case "base":
			if d, err := time.ParseDuration(v); err == nil {
				p.BackoffBase = d
			}
		case "max":
			if d, err := time.ParseDuration(v); err == nil {
				p.BackoffMax = d
			}
		}
	}
}

func applyBreakerSpec(p *Modifiers, spec string) {
	if spec == "" {
		return
	}
	if !strings.Contains(spec, "=") {
		if d, err := time.ParseDuration(spec); err == nil {
			p.BreakerCooldown = d
		}
		return
	}
	for _, part := range splitCSV(spec) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch k {
		case "failures":
			if n, err := strconv.Atoi(v); err == nil {
				p.BreakerFailures = n
			}
		case "cooldown":
			if d, err := time.ParseDuration(v); err == nil {
				p.BreakerCooldown = d
			}
		}
	}
}

func parseRPS(s string) float64 {
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSuffix(s, "/sec")
	s = strings.TrimSuffix(s, "rps")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}
