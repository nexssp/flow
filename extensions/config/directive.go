package config

import (
	"context"
	"strings"

	"github.com/nexssp/flow/core"
)

// ConfigDirective parses both the single-line form
// `@config:key=value` and the block form `@config { key: value, ... }`.
// Each pair is written to meta["config"] under its key.
var ConfigDirective = core.Directive{
	Name:    "config",
	Example: "@config:strict=true\n@config {\n  retries: 3 :cli=\"r\"\n}",
	Handler: handleConfig,
}

func handleConfig(_ context.Context, req core.DirectiveReq) (core.DirectiveRes, error) {
	cfg := metaConfig(req.Out)
	line := strings.TrimSpace(req.Lines[req.I])

	if openIndex := strings.IndexByte(line, '{'); openIndex >= 0 {
		if closeIndex := strings.IndexByte(line[openIndex+1:], '}'); closeIndex >= 0 {
			parseConfigPairs(line[openIndex+1:openIndex+1+closeIndex], cfg)
			return core.DirectiveRes{Next: req.I + 1}, nil
		}
		next := req.I + 1
		for next < len(req.Lines) {
			current := strings.TrimSpace(req.Lines[next])
			if current == "}" || strings.HasPrefix(current, "}") {
				next++
				break
			}
			parseConfigLine(current, cfg)
			next++
		}
		return core.DirectiveRes{Next: next}, nil
	}

	rest := strings.TrimPrefix(line, "@config:")
	if eq := strings.IndexByte(rest, '='); eq > 0 {
		key := strings.TrimSpace(rest[:eq])
		value := strings.Trim(strings.TrimSpace(rest[eq+1:]), `"'`)
		if key != "" {
			cfg[key] = value
		}
	}
	return core.DirectiveRes{Next: req.I + 1}, nil
}

func parseConfigPairs(body string, cfg map[string]string) {
	for _, pair := range core.SplitTopLevel(body, ',') {
		parseConfigLine(pair, cfg)
	}
}

// parseConfigLine parses one `key: value` pair. Inline modifiers of the
// form ` :cli="..."` are stripped before the value is stored.
func parseConfigLine(line string, cfg map[string]string) {
	line = strings.TrimSuffix(strings.TrimSpace(line), ",")
	colonIndex := strings.IndexByte(line, ':')
	if colonIndex <= 0 {
		return
	}
	key := strings.TrimSpace(line[:colonIndex])
	if key == "" || strings.HasPrefix(key, "@") {
		return
	}
	raw := stripInlineModifiers(strings.TrimSpace(line[colonIndex+1:]))
	raw = strings.TrimSuffix(strings.TrimSpace(raw), ",")
	cfg[key] = strings.Trim(strings.TrimSpace(raw), `"', `)
}

// stripInlineModifiers truncates a value at the first ` :` outside
// quotes, turning `3 :cli="r"` into `3`.
func stripInlineModifiers(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case ' ':
			if i+1 < len(s) && s[i+1] == ':' {
				return strings.TrimRight(s[:i], " \t")
			}
		}
	}
	return s
}
