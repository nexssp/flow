package flow

import (
	"os"
	"strconv"
)

type envConfig struct {
	Verbosity    *int
	BudgetMicros *int64
	Approval     *string
	MaxTokens    *int
	Observe      *string
	Provider     *string
	Model        *string
	Sandbox      *string
	OutputFormat *string
	OutputDir    *string
}

// readEnvConfig reads NEXSS_* variables. Returns nil pointers for
// unset variables; applyEnv only overrides Config when a pointer is
// non-nil.
func readEnvConfig() envConfig {
	var out envConfig

	if v := os.Getenv("NEXSS_VERBOSITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			out.Verbosity = &n
		}
	}

	if v := os.Getenv("NEXSS_BUDGET_MICROS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			out.BudgetMicros = &n
		}
	} else if v := os.Getenv("NEXSS_BUDGET_USD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			n := int64(f * 1_000_000)
			out.BudgetMicros = &n
		}
	}

	if v := os.Getenv("NEXSS_APPROVAL"); v != "" {
		out.Approval = &v
	}

	if v := os.Getenv("NEXSS_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.MaxTokens = &n
		}
	}

	if v := os.Getenv("NEXSS_OBSERVE"); v != "" {
		out.Observe = &v
	}

	if v := os.Getenv("NEXSS_PROVIDER"); v != "" {
		out.Provider = &v
	}

	if v := os.Getenv("NEXSS_MODEL"); v != "" {
		out.Model = &v
	}

	if v := os.Getenv("NEXSS_SANDBOX"); v != "" {
		out.Sandbox = &v
	}

	if v := os.Getenv("NEXSS_OUT"); v != "" {
		out.OutputFormat = &v
	}

	if v := os.Getenv("NEXSS_OUT_DIR"); v != "" {
		out.OutputDir = &v
	}

	return out
}

func applyEnv(cfg *Config, e envConfig, prov map[string]Layer) {
	if e.Verbosity != nil {
		cfg.Verbosity = *e.Verbosity
		prov["verbosity"] = LayerEnv
	}

	if e.BudgetMicros != nil {
		cfg.BudgetMicros = *e.BudgetMicros
		prov["budget"] = LayerEnv
	}

	if e.Approval != nil {
		cfg.Approval = *e.Approval
		prov["approval"] = LayerEnv
	}

	if e.MaxTokens != nil {
		cfg.MaxTokens = *e.MaxTokens
		prov["max_tokens"] = LayerEnv
	}

	if e.Observe != nil {
		cfg.Observe = *e.Observe
		prov["observe"] = LayerEnv
	}

	if e.Provider != nil {
		cfg.Provider = *e.Provider
		prov["provider"] = LayerEnv
	}

	if e.Model != nil {
		cfg.Model = *e.Model
		prov["model"] = LayerEnv
	}

	if e.Sandbox != nil {
		cfg.Sandbox = *e.Sandbox
		prov["sandbox"] = LayerEnv
	}

	if e.OutputFormat != nil {
		cfg.OutputFormat = *e.OutputFormat
		prov["output_format"] = LayerEnv
	}

	if e.OutputDir != nil {
		cfg.OutputDir = *e.OutputDir
		prov["output_dir"] = LayerEnv
	}
}
