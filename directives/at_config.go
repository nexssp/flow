package directives

import (
	"strings"
)

type atConfig struct{}

func init() { Register(atConfig{}) }

func (atConfig) Name() string { return "config" }

// ConfigFieldSpec holds metadata for a field declared inside @config { ... }.
type ConfigFieldSpec struct {
	Pos        Position
	Key        string
	Default    string
	CLI        string
	Desc       string
	Positional int
}

const configFieldsKey = "config_fields"

// Apply parses both single-line @config:key=value and multi-line @config { ... } blocks.
func (atConfig) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	if strings.Contains(line, "{") {
		return applyConfigBlock(ctx, lines, i)
	}

	return applyConfigSingleLine(ctx, lines, i)
}

func applyConfigSingleLine(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	spec, ok := StripDirectivePrefix(line, "config")
	if !ok {
		return 0, AtErr(ctx, i, "config", "malformed directive")
	}

	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return 0, AtErrf(ctx, i, "config", "expected key=value, got %q", spec)
	}

	key := strings.TrimSpace(spec[:eq])
	if key == "" {
		return 0, AtErrf(ctx, i, "config", "empty key in %q", spec)
	}

	val := TrimQuotes(strings.TrimSpace(spec[eq+1:]))
	ctx.Out.Config[key] = val

	if i < len(ctx.Body) {
		ctx.Body[i] = lines[i]
	}

	return i + 1, nil
}

func applyConfigBlock(ctx *Context, lines []string, i int) (int, error) {
	header, body, next, err := SplitBlock(lines, i)
	if err != nil {
		return 0, AtErr(ctx, i, "config", err.Error())
	}

	if _, ok := StripDirectivePrefix(header, "config"); !ok {
		return 0, AtErr(ctx, i, "config", "malformed block header")
	}

	var specs []ConfigFieldSpec

	for offset, rawLine := range body {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		bodyLine := i + offset + 2

		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			return 0, AtErrf(ctx, bodyLine, "config", "expected `key: value`, got %q", line)
		}

		key := strings.TrimSpace(line[:colonIdx])
		if key == "" {
			return 0, AtErrf(ctx, bodyLine, "config", "empty key on line %q", line)
		}

		rest := strings.TrimSpace(line[colonIdx+1:])
		valPart, modPart := splitValueAndModifiers(rest)

		val := TrimQuotes(strings.TrimSpace(valPart))
		ctx.Out.Config[key] = val

		spec := ConfigFieldSpec{
			Pos:     Position{File: ctx.File, Line: bodyLine},
			Key:     key,
			Default: val,
		}
		parseConfigModifiers(modPart, &spec)
		specs = append(specs, spec)
	}

	existing, _ := ctx.Out.Declarations[configFieldsKey].([]ConfigFieldSpec)
	ctx.Out.Declarations[configFieldsKey] = append(existing, specs...)

	return next, nil
}

// splitValueAndModifiers separates the default value from trailing :cli= or :desc= modifiers.
func splitValueAndModifiers(s string) (valuePart, modifierPart string) {
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
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
			}
		}
	}

	return strings.TrimSpace(s), ""
}
func parseConfigModifiers(modPart string, spec *ConfigFieldSpec) {
	if modPart == "" {
		return
	}

	var tokens []string
	inQuote := byte(0)
	start := -1

	for i := 0; i < len(modPart); i++ {
		c := modPart[i]
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			inQuote = c
			continue
		}
		if c == ':' {
			if start != -1 {
				tokens = append(tokens, strings.TrimSpace(modPart[start:i]))
			}
			start = i
		}
	}
	if start != -1 {
		tokens = append(tokens, strings.TrimSpace(modPart[start:]))
	}

	for _, token := range tokens {
		if !strings.HasPrefix(token, ":") {
			continue
		}
		raw := token[1:]
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(raw[:eq]))
		v := TrimQuotes(strings.TrimSpace(raw[eq+1:]))

		switch k {
		case "cli":
			spec.CLI = v
		case "desc", "description":
			spec.Desc = v
		case "positional":
			if n, err := ParseInt(v); err == nil {
				spec.Positional = n
			}
		case "default":
			if spec.Default == "" {
				spec.Default = v
			}
		}
	}
}
