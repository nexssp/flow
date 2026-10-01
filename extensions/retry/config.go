package retry

import (
	"strconv"
	"time"
)

// Config controls the retry policy installed by the adviser.
type Config struct {
	DefaultMax int
	MinBackoff time.Duration
	MaxBackoff time.Duration
	RetryAll   bool
}

// normalized fills every zero field with a working default. The
// returned Config is safe to use as-is.
func (c Config) normalized() Config {
	if c.DefaultMax <= 0 {
		c.DefaultMax = 3
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = 10 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 2 * time.Second
	}
	if c.MaxBackoff < c.MinBackoff {
		c.MaxBackoff = c.MinBackoff
	}
	return c
}

// ConfigFromOptions reads a `@require retry { ... }` options map. Only
// default_max is recognized today; unknown keys are ignored so future
// options do not break existing .nflow files.
func ConfigFromOptions(opts map[string]string) Config {
	var cfg Config
	if raw, ok := opts["default_max"]; ok {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			cfg.DefaultMax = parsed
		}
	}
	return cfg
}
