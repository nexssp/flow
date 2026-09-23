package flow

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/flow/directives/core"
)

// BuildResolvedConfig resolves configuration by applying precedence:
// CLI flags > Environment variables > DSL @config block/lines > defaults.
func BuildResolvedConfig(pre *core.Preprocessed, cliArgs []string) map[string]string {
	resolved := make(map[string]string)

	if pre != nil {
		for key, val := range pre.Config {
			resolved[key] = val
		}
	}

	// 1. Environment variables (NEXSS_<KEY>)
	for key := range resolved {
		envKey := "NEXSS_" + strings.ToUpper(key)
		if envVal, exists := os.LookupEnv(envKey); exists {
			resolved[key] = envVal
		}
	}

	// 2. CLI flags from @config { ... :cli="name,alias" ... }
	if pre != nil && len(cliArgs) > 0 {
		cliFlags := parseCLIArgsMap(cliArgs)
		if raw, exists := pre.Declarations["config_fields"]; exists {
			if specs, ok := raw.([]directives.ConfigFieldSpec); ok {
				for _, spec := range specs {
					if spec.CLI == "" {
						continue
					}
					for _, alias := range strings.Split(spec.CLI, ",") {
						alias = strings.TrimSpace(alias)
						if val, found := cliFlags[alias]; found {
							resolved[spec.Key] = val
							break
						}
					}
				}
			}
		}
	}

	return resolved
}

func parseCLIArgsMap(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		raw := strings.TrimLeft(arg, "-")
		if raw == "" {
			continue
		}

		if eq := strings.IndexByte(raw, '='); eq >= 0 {
			flags[raw[:eq]] = raw[eq+1:]
			continue
		}

		// Flag without '=' (boolean or followed by value)
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags[raw] = args[i+1]
			i++
		} else {
			flags[raw] = "true"
		}
	}
	return flags
}

// ResolveParamRef evaluates @config.key, @env.NAME, @flag.name, @arg.N, or returns literal.
func ResolveParamRef(raw string, opts *compileOptions) any {
	trimmed := strings.Trim(strings.TrimSpace(raw), `"'`)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "@") || strings.HasPrefix(trimmed, "!@") {
		if resolved, ok := resolveReference(trimmed, opts); ok {
			return resolved
		}
	}

	return CoerceLiteralValue(trimmed)
}

func resolveReference(ref string, opts *compileOptions) (any, bool) {
	if opts == nil {
		return ref, false
	}

	negate := false
	if strings.HasPrefix(ref, "!") {
		negate = true
		ref = ref[1:]
	}

	switch {
	case strings.HasPrefix(ref, "@config."):
		key := strings.TrimPrefix(ref, "@config.")
		val, exists := opts.config[key]
		if !exists {
			return nil, false
		}
		coerced := CoerceLiteralValue(val)
		if negate {
			if b, ok := coerced.(bool); ok {
				return !b, true
			}
		}
		return coerced, true

	case strings.HasPrefix(ref, "@env."):
		envKey := strings.TrimPrefix(ref, "@env.")
		val := os.Getenv(envKey)
		return CoerceLiteralValue(val), true

	case strings.HasPrefix(ref, "@flag."):
		flagName := strings.TrimPrefix(ref, "@flag.")
		isSet := hasFlag(opts.cliArgs, flagName)
		if negate {
			return !isSet, true
		}
		return isSet, true

	case strings.HasPrefix(ref, "@arg."):
		posStr := strings.TrimPrefix(ref, "@arg.")
		idx, err := strconv.Atoi(posStr)
		if err != nil || idx < 0 {
			return nil, false
		}
		val, exists := getPositionalArg(opts.cliArgs, idx)
		if !exists {
			return "", true
		}
		return CoerceLiteralValue(val), true
	}

	return ref, false
}

func hasFlag(args []string, name string) bool {
	p1 := "--" + name
	p2 := "-" + name
	for _, a := range args {
		if a == p1 || a == p2 || strings.HasPrefix(a, p1+"=") || strings.HasPrefix(a, p2+"=") {
			return true
		}
	}
	return false
}

func getPositionalArg(args []string, targetIndex int) (string, bool) {
	currentPos := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		if currentPos == targetIndex {
			return arg, true
		}
		currentPos++
	}
	return "", false
}

// CoerceLiteralValue parses JSON arrays, objects, booleans, and numbers into native types.
func CoerceLiteralValue(trimmed string) any {
	if trimmed == "true" {
		return true
	}
	if trimmed == "false" {
		return false
	}

	if (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) ||
		(strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) {
		var out any
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			return out
		}
	}

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}

	return trimmed
}
