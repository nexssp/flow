package directives

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type atConfigLoad struct{}

func init() { Register(atConfigLoad{}) }

func (atConfigLoad) Name() string { return "config.load" }

type configLoadSpec struct {
	Path     string
	Prefix   string
	Required bool
}

func (atConfigLoad) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	specStr, ok := StripDirectivePrefix(line, "config.load")
	if !ok {
		return 0, AtErr(ctx, i, "config.load", "malformed directive")
	}

	spec := parseConfigLoadSpec(specStr)
	if spec.Path == "" {
		return 0, AtErr(ctx, i, "config.load", "path is required (e.g. @config.load:path=\"config.yml\")")
	}

	resolvedPath := spec.Path
	if !filepath.IsAbs(resolvedPath) && ctx.BaseDir != "" {
		resolvedPath = filepath.Join(ctx.BaseDir, resolvedPath)
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) && !spec.Required {
			return i + 1, nil // Optional configuration file missing: skip gracefully
		}
		return 0, AtErrf(ctx, i, "config.load", "read %q: %v", spec.Path, err)
	}

	ext := strings.ToLower(filepath.Ext(resolvedPath))
	var rawMap map[string]any

	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &rawMap); err != nil {
			return 0, AtErrf(ctx, i, "config.load", "invalid JSON in %q: %v", spec.Path, err)
		}
	case ".yml", ".yaml":
		if err := yaml.Unmarshal(data, &rawMap); err != nil {
			return 0, AtErrf(ctx, i, "config.load", "invalid YAML in %q: %v", spec.Path, err)
		}
	case ".toml":
		parsed, err := parseSimpleTOML(data)
		if err != nil {
			return 0, AtErrf(ctx, i, "config.load", "invalid TOML in %q: %v", spec.Path, err)
		}
		rawMap = parsed
	default:
		// Attempt YAML/JSON fallback
		if err := yaml.Unmarshal(data, &rawMap); err != nil {
			return 0, AtErrf(ctx, i, "config.load", "unsupported format %q for %q", ext, spec.Path)
		}
	}

	if ctx.Out.Config == nil {
		ctx.Out.Config = make(map[string]string)
	}

	flattenConfig(spec.Prefix, rawMap, ctx.Out.Config)

	if i < len(ctx.Body) {
		ctx.Body[i] = lines[i]
	}

	return i + 1, nil
}

func parseConfigLoadSpec(s string) configLoadSpec {
	spec := configLoadSpec{Required: true}
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, ":") {
		s = ":" + s
	}

	tokens := strings.Fields(s)
	for _, token := range tokens {
		if !strings.HasPrefix(token, ":") {
			continue
		}
		raw := token[1:]
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(raw[:eq]))
		val := TrimQuotes(strings.TrimSpace(raw[eq+1:]))

		switch key {
		case "path":
			spec.Path = val
		case "prefix":
			spec.Prefix = val
		case "required":
			if b, err := strconv.ParseBool(val); err == nil {
				spec.Required = b
			}
		}
	}

	return spec
}

func flattenConfig(prefix string, value any, out map[string]string) {
	switch v := value.(type) {
	case map[string]any:
		for k, sub := range v {
			subKey := k
			if prefix != "" {
				subKey = prefix + "." + k
			}
			flattenConfig(subKey, sub, out)
		}
	case map[any]any:
		for k, sub := range v {
			keyStr := fmt.Sprint(k)
			subKey := keyStr
			if prefix != "" {
				subKey = prefix + "." + keyStr
			}
			flattenConfig(subKey, sub, out)
		}
	case []any:
		if prefix != "" {
			if b, err := json.Marshal(v); err == nil {
				out[prefix] = string(b)
			}
		}
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(v)
		}
	}
}

// parseSimpleTOML is a minimal TOML reader that handles only the two
// constructs a config file realistically needs: section headers of the
// form [a.b.c], and key = value pairs. Values are kept as strings;
// callers that need typed values coerce them downstream.
//
// Full TOML (dates, arrays, inline tables, multi-line strings) is not
// supported. That is a deliberate boundary: config.load reads small
// files, and pulling in a full TOML parser for that is not worth the
// dependency weight.
func parseSimpleTOML(data []byte) (map[string]any, error) {
	result := make(map[string]any)
	currentMap := result

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header: [section] or [section.subsection].
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			currentMap = ensureNestedMap(result, section)
			continue
		}

		// Key = value.
		eq := strings.IndexByte(line, '=')
		if eq > 0 {
			k := strings.TrimSpace(line[:eq])
			v := strings.TrimSpace(line[eq+1:])
			v = TrimQuotes(v)
			currentMap[k] = v
		}
	}

	return result, scanner.Err()
}

func ensureNestedMap(root map[string]any, section string) map[string]any {
	parts := strings.Split(section, ".")
	curr := root

	for _, p := range parts {
		if sub, ok := curr[p].(map[string]any); ok {
			curr = sub
		} else {
			newSub := make(map[string]any)
			curr[p] = newSub
			curr = newSub
		}
	}

	return curr
}
