package flow

import (
	"strconv"
	"strings"
)

type cliConfig struct {
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

// parseCLIConfig reads config knobs from a raw flag slice. Unknown
// flags are ignored so the runner can reuse the same args for its own
// parsing (path, payload, --info, --assert=, …).
func parseCLIConfig(args []string) cliConfig {
	var out cliConfig

	for _, arg := range args {
		switch {
		case arg == "-v" || arg == "--verbose":
			n := 1
			out.Verbosity = &n
		case arg == "-vv":
			n := 2
			out.Verbosity = &n
		case arg == "-vvv":
			n := 3
			out.Verbosity = &n
		case arg == "-q" || arg == "--quiet":
			n := 0
			out.Verbosity = &n
		case strings.HasPrefix(arg, "--verbose="):
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--verbose=")); err == nil && n >= 0 {
				out.Verbosity = &n
			}
		case strings.HasPrefix(arg, "--budget-micros="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(arg, "--budget-micros="), 10, 64); err == nil && n > 0 {
				out.BudgetMicros = &n
			}
		case strings.HasPrefix(arg, "--budget="):
			if f, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--budget="), 64); err == nil && f > 0 {
				n := int64(f * 1_000_000)
				out.BudgetMicros = &n
			}
		case strings.HasPrefix(arg, "--max-tokens="):
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-tokens=")); err == nil && n > 0 {
				out.MaxTokens = &n
			}
		case strings.HasPrefix(arg, "--approval="):
			v := strings.TrimPrefix(arg, "--approval=")
			out.Approval = &v
		case strings.HasPrefix(arg, "--observe="):
			v := strings.TrimPrefix(arg, "--observe=")
			out.Observe = &v
		case strings.HasPrefix(arg, "--provider="):
			v := strings.TrimPrefix(arg, "--provider=")
			out.Provider = &v
		case strings.HasPrefix(arg, "--model="):
			v := strings.TrimPrefix(arg, "--model=")
			out.Model = &v
		case strings.HasPrefix(arg, "--sandbox="):
			v := strings.TrimPrefix(arg, "--sandbox=")
			out.Sandbox = &v
		case strings.HasPrefix(arg, "--out="):
			v := strings.TrimPrefix(arg, "--out=")
			out.OutputFormat = &v
		case strings.HasPrefix(arg, "--out-dir="):
			v := strings.TrimPrefix(arg, "--out-dir=")
			out.OutputDir = &v
		}
	}

	return out
}

func applyCLI(cfg *Config, c cliConfig, prov map[string]Layer) {
	if c.Verbosity != nil {
		cfg.Verbosity = *c.Verbosity
		prov["verbosity"] = LayerCLI
	}

	if c.BudgetMicros != nil {
		cfg.BudgetMicros = *c.BudgetMicros
		prov["budget"] = LayerCLI
	}

	if c.Approval != nil {
		cfg.Approval = *c.Approval
		prov["approval"] = LayerCLI
	}

	if c.MaxTokens != nil {
		cfg.MaxTokens = *c.MaxTokens
		prov["max_tokens"] = LayerCLI
	}

	if c.Observe != nil {
		cfg.Observe = *c.Observe
		prov["observe"] = LayerCLI
	}

	if c.Provider != nil {
		cfg.Provider = *c.Provider
		prov["provider"] = LayerCLI
	}

	if c.Model != nil {
		cfg.Model = *c.Model
		prov["model"] = LayerCLI
	}

	if c.Sandbox != nil {
		cfg.Sandbox = *c.Sandbox
		prov["sandbox"] = LayerCLI
	}

	if c.OutputFormat != nil {
		cfg.OutputFormat = *c.OutputFormat
		prov["output_format"] = LayerCLI
	}

	if c.OutputDir != nil {
		cfg.OutputDir = *c.OutputDir
		prov["output_dir"] = LayerCLI
	}
}
