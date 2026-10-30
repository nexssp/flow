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

	spec := parseConfigLoadSpec(rest)
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

// parseConfigLoadSpec reads `:key="value"` tokens. Only path, prefix,
// and required are meaningful; other tokens are ignored.
func parseConfigLoadSpec(spec string) configLoadSpec {
	parsed := configLoadSpec{Required: true}
	spec = strings.TrimSpace(spec)
	if !strings.HasPrefix(spec, ":") {
		spec = ":" + spec
	}
	for token := range strings.FieldsSeq(spec) {
		if !strings.HasPrefix(token, ":") {
			continue
		}
		raw := token[1:]
		before, after, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(before))
		value := compiler.TrimQuotes(strings.TrimSpace(after))
		switch key {
		case "path":
			parsed.Path = value
		case "prefix":
			parsed.Prefix = value
		case "required":
			if b, err := strconv.ParseBool(value); err == nil {
				parsed.Required = b
			}
		}
	}
	return parsed
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

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = ensureNestedMap(result, strings.TrimSpace(line[1:len(line)-1]))
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := compiler.TrimQuotes(strings.TrimSpace(line[eq+1:]))
		current[key] = value
	}
	return result, scanner.Err()
}

func ensureNestedMap(root map[string]any, section string) map[string]any {
	current := root
	for part := range strings.SplitSeq(section, ".") {
		if child, ok := current[part].(map[string]any); ok {
			current = child
			continue
		}
		child := make(map[string]any)
		current[part] = child
		current = child
	}
	return current
}
