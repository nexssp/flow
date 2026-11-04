package config

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

// ConfigLoadDirective parses `@config.load:path="file.yml" [prefix="x"]`
// and flattens the loaded document into meta["config"], joining nested
// keys with dots.
var ConfigLoadDirective = compiler.Directive{
	Name:    "config.load",
	Example: `@config.load:path="nexss.yml"`,
	Handler: handleConfigLoad,
}

type configLoadSpec struct {
	Path     string
	Prefix   string
	Required bool
}

func handleConfigLoad(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
	pos := compiler.Position{File: req.File, Line: req.I + 1}
	line := strings.TrimSpace(req.Lines[req.I])

	rest, ok := stripDirectivePrefix(line, "@config.load")
	if !ok {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@config.load: malformed directive")
	}

	spec, err := parseConfigLoadSpec(rest)
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@config.load: %v", err)
	}
	if spec.Path == "" {
		return compiler.DirectiveRes{}, compiler.SourceError(pos,
			`@config.load: path is required (use: @config.load:path="file.yml")`)
	}

	path := spec.Path
	if !filepath.IsAbs(path) && req.BaseDir != "" {
		path = filepath.Join(req.BaseDir, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !spec.Required {
			return compiler.DirectiveRes{Next: req.I + 1}, nil
		}
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@config.load: read %q: %v", spec.Path, err)
	}

	raw, err := decodeConfigFile(data, strings.ToLower(filepath.Ext(path)))
	if err != nil {
		return compiler.DirectiveRes{}, compiler.SourceError(pos, "@config.load: parse %q: %v", spec.Path, err)
	}

	flattenConfig(spec.Prefix, raw, metaConfig(req.Out))
	return compiler.DirectiveRes{Next: req.I + 1}, nil
}

// stripDirectivePrefix returns the text after `@name`, tolerating an
// optional whitespace or colon separator.
func stripDirectivePrefix(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	rest := line[len(prefix):]
	if rest == "" {
		return "", true
	}
	switch rest[0] {
	case ' ', '\t', ':':
		return strings.TrimSpace(rest[1:]), true
	}
	return "", false
}

// parseConfigLoadSpec reads `:key="value"` tokens. Unknown options are ignored.
func parseConfigLoadSpec(spec string) (configLoadSpec, error) {
	parsed := configLoadSpec{Required: true}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return parsed, nil
	}
	if !strings.HasPrefix(spec, ":") {
		spec = ":" + spec
	}
	tokens, err := splitConfigLoadOptions(spec)
	if err != nil {
		return parsed, err
	}
	seen := make(map[string]struct{}, 3)
	for _, token := range tokens {
		if !strings.HasPrefix(token, ":") {
			continue
		}
		key, value, ok := strings.Cut(token[1:], "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok {
			switch key {
			case "path", "prefix", "required":
				return parsed, fmt.Errorf("option %q requires a value", key)
			}
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" && strings.ContainsRune("\"'`", rune(value[0])) && compiler.TrimQuotes(value) == value {
			return parsed, fmt.Errorf("option %q has an unterminated quoted value", key)
		}
		value = compiler.TrimQuotes(value)
		switch key {
		case "path", "prefix", "required":
			if _, exists := seen[key]; exists {
				return parsed, fmt.Errorf("option %q is repeated", key)
			}
			seen[key] = struct{}{}
		}
		switch key {
		case "path":
			parsed.Path = value
		case "prefix":
			parsed.Prefix = value
		case "required":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return parsed, fmt.Errorf("invalid required value %q", value)
			}
			parsed.Required = b
		}
	}
	return parsed, nil
}

func splitConfigLoadOptions(spec string) ([]string, error) {
	var tokens []string
	var quote byte
	start := -1
	escaped := false
	for i := range len(spec) {
		current := spec[i]
		if quote != 0 {
			if quote == '"' && current == '\\' && !escaped {
				escaped = true
				continue
			}
			if current == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch current {
		case '"', '\'', '`':
			if start < 0 {
				start = i
			}
			quote = current
		case ' ', '\t', '\r', '\n':
			if start >= 0 {
				tokens = append(tokens, spec[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quoted value")
	}
	if start >= 0 {
		tokens = append(tokens, spec[start:])
	}
	return tokens, nil
}

// DecodeFile dispatches by extension: .json → encoding/json, .toml → the
// minimal TOML parser, and .yaml/.yml → the registered YAML loader from
// extensions/config_yaml. It exposes structured values to runtime extensions
// without changing compile-time @config.load semantics.
func DecodeFile(data []byte, ext string) (map[string]any, error) {
	ext = strings.ToLower(ext)
	if (ext == ".yaml" || ext == ".yml") && yamlLoader.Load() == nil {
		return nil, errors.New("YAML requires the optional config_yaml decoder")
	}
	return decodeConfigFile(data, ext)
}

// decodeConfigFile dispatches by extension for compile-time @config.load.
func decodeConfigFile(data []byte, ext string) (map[string]any, error) {
	switch ext {
	case ".json":
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	case ".toml":
		return parseSimpleTOML(data)
	case ".yaml", ".yml":
		loader := yamlLoader.Load()
		if loader == nil {
			return nil, errors.New("@config.load: YAML requires @require config_yaml")
		}
		return (*loader)(data)
	default:
		return nil, fmt.Errorf("unsupported config extension: %s", ext)
	}
}

// flattenConfig writes nested maps into out using dot-joined keys.
// Arrays are JSON-encoded into a single string. Scalars are fmt.Sprint-ed.
func flattenConfig(prefix string, value any, out map[string]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, sub := range v {
			flattenConfig(joinPrefix(prefix, key), sub, out)
		}
	case map[any]any:
		for key, sub := range v {
			flattenConfig(joinPrefix(prefix, fmt.Sprint(key)), sub, out)
		}
	case []any:
		if prefix != "" {
			if encoded, err := json.Marshal(v); err == nil {
				out[prefix] = string(encoded)
			}
		}
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(v)
		}
	}
}

func joinPrefix(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// parseSimpleTOML handles the subset used by Nexss config files:
// [section] headers and `key = value` pairs. Dotted paths in headers
// nest tables. Arrays and inline tables are not supported.
func parseSimpleTOML(data []byte) (map[string]any, error) {
	result := make(map[string]any)
	current := result
	sections := make(map[string]struct{})

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var err error
		current, err = parseSimpleTOMLLine(line, lineNumber, result, current, sections)
		if err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
	}
	return result, nil
}

func parseSimpleTOMLLine(line string, lineNumber int, root, current map[string]any, sections map[string]struct{}) (map[string]any, error) {
	if strings.HasPrefix(line, "[") || strings.HasSuffix(line, "]") {
		if len(line) < 3 || !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			return current, fmt.Errorf("line %d: malformed table header", lineNumber)
		}
		section := strings.TrimSpace(line[1 : len(line)-1])
		if section == "" || strings.ContainsAny(section, "[]\"'`") {
			return current, fmt.Errorf("line %d: unsupported table name %q", lineNumber, section)
		}
		if _, exists := sections[section]; exists {
			return current, fmt.Errorf("line %d: duplicate table %q", lineNumber, section)
		}
		nested, err := ensureNestedMap(root, section)
		if err != nil {
			return current, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		sections[section] = struct{}{}
		return nested, nil
	}
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return current, fmt.Errorf("line %d: expected key = value", lineNumber)
	}
	key := strings.TrimSpace(line[:eq])
	value := strings.TrimSpace(line[eq+1:])
	if key == "" || value == "" {
		return current, fmt.Errorf("line %d: key and value are required", lineNumber)
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		return current, fmt.Errorf("line %d: arrays and inline tables are unsupported", lineNumber)
	}
	if strings.ContainsRune("\"'", rune(value[0])) && compiler.TrimQuotes(value) == value {
		return current, fmt.Errorf("line %d: malformed quoted value", lineNumber)
	}
	if _, exists := current[key]; exists {
		return current, fmt.Errorf("line %d: duplicate key %q", lineNumber, key)
	}
	current[key] = compiler.TrimQuotes(value)
	return current, nil
}

func ensureNestedMap(root map[string]any, section string) (map[string]any, error) {
	current := root
	for part := range strings.SplitSeq(section, ".") {
		if part == "" {
			return nil, errors.New("table name contains an empty component")
		}
		if child, ok := current[part].(map[string]any); ok {
			current = child
			continue
		}
		if _, exists := current[part]; exists {
			return nil, fmt.Errorf("table %q conflicts with a value", part)
		}
		child := make(map[string]any)
		current[part] = child
		current = child
	}
	return current, nil
}
