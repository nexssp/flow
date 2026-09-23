package directives

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/directives/core"
)

type atRequire struct{}

func init() { Register(atRequire{}) }

func (atRequire) Name() string { return "require" }

// Syntax:
//
//	@require ./lib/text
//	@require github.com/acme/text-tools v1.0.0
//
// Local paths resolve to the containing module path by walking up to
// the nearest go.mod. Remote modules carry an explicit version.
func (atRequire) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	var (
		header  string
		body    []string
		next    int
		options map[string]string
		err     error
	)

	// Directives with options blocks use SplitBlock to extract block body
	if strings.Contains(line, "{") {
		header, body, next, err = SplitBlock(lines, i)
		if err != nil {
			return 0, AtErr(ctx, i, "require", err.Error())
		}
		options, err = parseRequireOptions(body, ctx, i)
		if err != nil {
			return 0, err
		}
	} else {
		header = line
		next = i + 1
	}

	spec, ok := StripDirectivePrefix(header, "require")
	if !ok {
		return 0, AtErr(ctx, i, "require", "malformed directive")
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, AtErr(ctx, i, "require", "requires a path")
	}

	requirement, err := parseRequirement(spec, ctx.BaseDir)
	if err != nil {
		return 0, AtErr(ctx, i, "require", err.Error())
	}
	requirement.Options = options
	requirement.Pos = Position{File: ctx.File, Line: i + 1}

	ctx.Out.Requires = appendUniqueRequirement(ctx.Out.Requires, requirement)
	return next, nil
}

func parseRequireOptions(body []string, ctx *Context, baseLine int) (map[string]string, error) {
	if len(body) == 0 {
		return nil, nil
	}
	options := make(map[string]string)
	for offset, rawLine := range body {
		trimmedLine := strings.TrimSpace(rawLine)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") || strings.HasPrefix(trimmedLine, "//") {
			continue
		}
		bodyLine := baseLine + offset + 1

		for _, pair := range core.SplitTopLevel(trimmedLine, ',') {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			colonIndex := strings.IndexByte(pair, ':')
			if colonIndex < 0 {
				return nil, AtErrf(ctx, bodyLine, "require", "expected `key: value`, got %q", pair)
			}
			key := TrimQuotes(strings.TrimSpace(pair[:colonIndex]))
			value := TrimQuotes(strings.TrimSpace(pair[colonIndex+1:]))
			if key == "" {
				return nil, AtErrf(ctx, bodyLine, "require", "empty option key in %q", pair)
			}
			options[key] = value
		}
	}
	return options, nil
}

func parseRequirement(spec, baseDir string) (Requirement, error) {
	spec = strings.TrimSpace(spec)
	parts := strings.Fields(spec)
	switch len(parts) {
	case 1:
		raw := strings.Trim(parts[0], `"'`)
		if raw == "" {
			return Requirement{}, fmt.Errorf("empty path")
		}
		// Support `@require "github.com/org/repo@v1.0.0"` syntax
		if atIndex := strings.LastIndexByte(raw, '@'); atIndex > 0 && !looksLikeLocalPath(raw) {
			importPath := raw[:atIndex]
			version := raw[atIndex+1:]
			if !strings.HasPrefix(version, "v") {
				return Requirement{}, fmt.Errorf("version must start with 'v': %q", version)
			}
			return Requirement{Import: importPath, Version: version}, nil
		}
		if !looksLikeLocalPath(raw) {
			return Requirement{}, fmt.Errorf(
				"remote @require needs a version: `@require %s v1.2.3`", raw)
		}
		return resolveLocalRequirement(raw, baseDir)

	case 2:
		importPath := strings.Trim(parts[0], `"'`)
		version := strings.Trim(parts[1], `"'`)
		if importPath == "" || version == "" {
			return Requirement{}, fmt.Errorf("malformed: %s", spec)
		}
		if looksLikeLocalPath(importPath) {
			return Requirement{}, fmt.Errorf(
				"local @require must not carry a version: `@require %s`", importPath)
		}
		if !strings.HasPrefix(version, "v") {
			return Requirement{}, fmt.Errorf(
				"version must start with 'v': `@require %s %s`", importPath, version)
		}
		return Requirement{Import: importPath, Version: version}, nil

	default:
		return Requirement{}, fmt.Errorf("expects `path` or `path version`, got: %s", spec)
	}
}

func resolveLocalRequirement(importPath, baseDir string) (Requirement, error) {
	if baseDir == "" {
		return Requirement{}, fmt.Errorf(
			"local @require is not available in byte-source mode")
	}

	target := importPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return Requirement{}, fmt.Errorf("resolve %s: %w", importPath, err)
	}

	moduleRoot, modulePath, err := findModuleRoot(abs)
	if err != nil {
		return Requirement{}, fmt.Errorf("%s: %w", importPath, err)
	}

	rel, err := filepath.Rel(moduleRoot, abs)
	if err != nil {
		return Requirement{}, fmt.Errorf("%s: %w", importPath, err)
	}

	importCanonical := modulePath
	if rel != "." && rel != "" {
		importCanonical = modulePath + "/" + filepath.ToSlash(rel)
	}

	return Requirement{
		Import:     importCanonical,
		LocalPath:  abs,
		ModuleRoot: moduleRoot,
		ModulePath: modulePath,
	}, nil
}

func findModuleRoot(dir string) (string, string, error) {
	curr := dir
	for {
		gomod := filepath.Join(curr, "go.mod")
		if _, err := os.Stat(gomod); err == nil {
			modulePath, err := readModuleLine(gomod)
			if err != nil {
				return "", "", err
			}
			return curr, modulePath, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			return "", "", fmt.Errorf(
				"no go.mod found above %s (local @require must point into a Go module)", dir)
		}
		curr = parent
	}
}

func readModuleLine(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s has no module directive", gomod)
}

func looksLikeLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, `.\`) ||
		strings.HasPrefix(s, `..\`)
}

func appendUniqueRequirement(dst []Requirement, r Requirement) []Requirement {
	for i, existing := range dst {
		if existing.Import != r.Import {
			continue
		}
		if existing.Version != r.Version && existing.Version != "" && r.Version != "" {
			return append(dst, r)
		}
		if len(r.Options) > 0 {
			if dst[i].Options == nil {
				dst[i].Options = make(map[string]string)
			}
			for key, value := range r.Options {
				dst[i].Options[key] = value
			}
		}
		return dst
	}
	return append(dst, r)
}
