package flow

import (
	"strconv"
	"strings"
)

// dslConfig holds the values found in @config directives.
type dslConfig struct {
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

func parseDSLConfig(text string) dslConfig {
	var out dslConfig
	lines := strings.Split(text, "\n")
	inBlock := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "@config") && strings.Contains(line, "{") {
			inBlock = true
			continue
		}

		if inBlock {
			if line == "}" || strings.HasPrefix(line, "}") {
				inBlock = false
				continue
			}

			colonIdx := strings.IndexByte(line, ':')
			if colonIdx > 0 {
				key := strings.TrimSpace(line[:colonIdx])
				rest := strings.TrimSpace(line[colonIdx+1:])
				val := extractBlockValue(rest)
				applyConfigKeyValue(&out, key, val)
			}
			continue
		}

		if strings.HasPrefix(line, "@config:") {
			kv := strings.TrimPrefix(line, "@config:")
			eq := strings.IndexByte(kv, '=')
			if eq > 0 {
				key := strings.TrimSpace(kv[:eq])
				val := strings.Trim(strings.TrimSpace(kv[eq+1:]), `"'`)
				applyConfigKeyValue(&out, key, val)
			}
		}
	}

	return out
}

func extractBlockValue(s string) string {
	inQuote := byte(0)
	depth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if inQuote != 0 {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}

		switch ch {
		case '"', '\'', '`':
			inQuote = ch
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ':':
			if depth == 0 && (i > 0 && (s[i-1] == ' ' || s[i-1] == '\t')) {
				return strings.Trim(strings.TrimSpace(s[:i]), `"'`)
			}
		}
	}

	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func applyConfigKeyValue(out *dslConfig, key, val string) {
	switch key {
	case "verbosity", "v":
		if n, err := strconv.Atoi(val); err == nil && n >= 0 {
			out.Verbosity = &n
		}
	case "budget_micros":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			out.BudgetMicros = &n
		}
	case "budget_usd", "budget":
		if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
			n := int64(f * 1_000_000)
			out.BudgetMicros = &n
		}
	case "approval":
		out.Approval = &val
	case "max_tokens":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			out.MaxTokens = &n
		}
	case "observe":
		out.Observe = &val
	case "provider":
		out.Provider = &val
	case "model":
		out.Model = &val
	case "sandbox":
		out.Sandbox = &val
	case "out", "output_format":
		out.OutputFormat = &val
	case "out_dir", "output_dir":
		out.OutputDir = &val
	}
}

func applyDSL(cfg *Config, d dslConfig, prov map[string]Layer) {
	if d.Verbosity != nil {
		cfg.Verbosity = *d.Verbosity
		prov["verbosity"] = LayerDSL
	}
	if d.BudgetMicros != nil {
		cfg.BudgetMicros = *d.BudgetMicros
		prov["budget"] = LayerDSL
	}
	if d.Approval != nil {
		cfg.Approval = *d.Approval
		prov["approval"] = LayerDSL
	}
	if d.MaxTokens != nil {
		cfg.MaxTokens = *d.MaxTokens
		prov["max_tokens"] = LayerDSL
	}
	if d.Observe != nil {
		cfg.Observe = *d.Observe
		prov["observe"] = LayerDSL
	}
	if d.Provider != nil {
		cfg.Provider = *d.Provider
		prov["provider"] = LayerDSL
	}
	if d.Model != nil {
		cfg.Model = *d.Model
		prov["model"] = LayerDSL
	}
	if d.Sandbox != nil {
		cfg.Sandbox = *d.Sandbox
		prov["sandbox"] = LayerDSL
	}
	if d.OutputFormat != nil {
		cfg.OutputFormat = *d.OutputFormat
		prov["output_format"] = LayerDSL
	}
	if d.OutputDir != nil {
		cfg.OutputDir = *d.OutputDir
		prov["output_dir"] = LayerDSL
	}
}
