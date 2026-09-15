package flow

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// ActionMeta describes a .flow file that declares itself as an action
// via @action and (optionally) @description.
type ActionMeta struct {
	Name        string
	Description string
}

// Pipeline is a named reusable subflow declared with @pipeline.
//
// Pipelines live as a slice, not a map: a duplicate name is a
// declaration error, and the order in which they were written matters
// for diagnostics. The registry later converts each into an action.
type Pipeline struct {
	Name string
	Body string
}

// Requirement is one @require directive, fully resolved.
//
// Two forms are accepted in .flow files:
//
//	Local:   @require ./relative/path
//	         @require ../shared/actions
//	Remote:  @require github.com/acme/text v1.0.0
//
// Local paths are resolved at preprocess time to a canonical Go module
// path by walking up from the target directory until a go.mod is found
// and reading its module line.
type Requirement struct {
	Import     string
	Version    string
	LocalPath  string
	ModuleRoot string
	ModulePath string
}

func (r Requirement) IsLocal() bool { return r.LocalPath != "" }

// Preprocessed is the result of resolving @include directives and
// extracting @pipeline / @action / @require metadata from a .flow source.
type Preprocessed struct {
	DSL       string
	Pipelines []Pipeline
	Action    *ActionMeta
	Includes  []string
	Requires  []Requirement
}

// Preprocess reads path, resolves @include directives recursively,
// extracts metadata, and returns the flow body with directives removed.
func Preprocess(path string) (*Preprocessed, error) {
	visited := make(map[string]bool)

	return preprocessFile(path, visited)
}

func preprocessFile(path string, visited map[string]bool) (*Preprocessed, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, xerr.BadRequest("flow: resolve " + path + ": " + err.Error())
	}

	if visited[abs] {
		return nil, xerr.Conflict("flow: @include cycle: " + abs)
	}

	visited[abs] = true
	defer delete(visited, abs)

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, xerr.NotFound("flow: read "+abs, err)
	}

	pre, err := preprocessSource(string(data), filepath.Dir(abs), visited)
	if err != nil {
		return nil, err
	}

	pre.Includes = append([]string{abs}, pre.Includes...)

	return pre, nil
}

func preprocessSource(src, baseDir string, visited map[string]bool) (*Preprocessed, error) {
	out := &Preprocessed{}

	var body []string

	lines := strings.Split(src, "\n")
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "@include "):
			ref := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "@include ")), `"'`)

			incPath := ref
			if !filepath.IsAbs(incPath) {
				incPath = filepath.Join(baseDir, incPath)
			}

			inc, err := preprocessFile(incPath, visited)
			if err != nil {
				return nil, xerr.BadRequest("flow: @include " + ref + ": " + err.Error())
			}

			for _, p := range inc.Pipelines {
				if _, exists := findPipeline(out.Pipelines, p.Name); exists {
					return nil, xerr.Conflict("flow: @pipeline " + p.Name +
						" declared in both this file and " + incPath)
				}

				out.Pipelines = append(out.Pipelines, p)
			}

			for _, r := range inc.Requires {
				out.Requires = appendUniqueRequirement(out.Requires, r)
			}

			if s := strings.TrimSpace(inc.DSL); s != "" {
				body = append(body, "("+s+")")
			}

			i++

			continue

		case strings.HasPrefix(trimmed, "@pipeline "):
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "@pipeline "))
			if name == "" {
				return nil, xerr.BadRequest("flow: @pipeline requires a name")
			}

			var pbody []string

			i++
			closed := false

			for i < len(lines) {
				if strings.TrimSpace(lines[i]) == "@end" {
					closed = true
					i++

					break
				}

				pbody = append(pbody, lines[i])
				i++
			}

			if !closed {
				return nil, xerr.BadRequest("flow: @pipeline " + name + " missing @end")
			}

			if _, exists := findPipeline(out.Pipelines, name); exists {
				return nil, xerr.Conflict("flow: @pipeline " + name + " declared twice")
			}

			out.Pipelines = append(out.Pipelines, Pipeline{
				Name: name,
				Body: strings.TrimSpace(strings.Join(pbody, "\n")),
			})

			continue

		case strings.HasPrefix(trimmed, "@action "):
			if out.Action == nil {
				out.Action = &ActionMeta{}
			}

			out.Action.Name = strings.Trim(
				strings.TrimSpace(strings.TrimPrefix(trimmed, "@action ")), `"'`)
			i++

			continue

		case strings.HasPrefix(trimmed, "@description "):
			if out.Action == nil {
				out.Action = &ActionMeta{}
			}

			out.Action.Description = strings.Trim(
				strings.TrimSpace(strings.TrimPrefix(trimmed, "@description ")), `"'`)
			i++

			continue

		case strings.HasPrefix(trimmed, "@require "):
			spec := strings.TrimSpace(strings.TrimPrefix(trimmed, "@require "))

			req, err := parseRequirement(spec, baseDir)
			if err != nil {
				return nil, err
			}

			out.Requires = appendUniqueRequirement(out.Requires, req)
			i++

			continue
		}

		body = append(body, line)
		i++
	}

	out.DSL = strings.Join(body, "\n")

	return out, nil
}

func findPipeline(ps []Pipeline, name string) (Pipeline, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}

	return Pipeline{}, false
}

// parseRequirement interprets one @require line.
func parseRequirement(spec, baseDir string) (Requirement, error) {
	parts := strings.Fields(spec)
	switch len(parts) {
	case 1:
		importPath := strings.Trim(parts[0], `"'`)
		if importPath == "" {
			return Requirement{}, xerr.BadRequest("flow: empty @require")
		}

		if !looksLikeLocalPath(importPath) {
			return Requirement{}, xerr.BadRequest(
				"flow: remote @require needs a version: `@require " + importPath + " v1.2.3`")
		}

		return resolveLocalRequirement(importPath, baseDir)

	case 2:
		importPath := strings.Trim(parts[0], `"'`)

		version := strings.Trim(parts[1], `"'`)
		if importPath == "" || version == "" {
			return Requirement{}, xerr.BadRequest("flow: malformed @require: " + spec)
		}

		if looksLikeLocalPath(importPath) {
			return Requirement{}, xerr.BadRequest(
				"flow: local @require must not carry a version: `@require " + importPath + "`")
		}

		if !strings.HasPrefix(version, "v") {
			return Requirement{}, xerr.BadRequest(
				"flow: version must start with 'v': `@require " + importPath + " " + version + "`")
		}

		return Requirement{Import: importPath, Version: version}, nil

	default:
		return Requirement{}, xerr.BadRequest(
			"flow: @require expects `path` or `path version`, got: " + spec)
	}
}

func resolveLocalRequirement(importPath, baseDir string) (Requirement, error) {
	target := importPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return Requirement{}, xerr.BadRequest(
			"flow: resolve local path " + importPath + ": " + err.Error())
	}

	moduleRoot, modulePath, err := findModuleRoot(abs)
	if err != nil {
		return Requirement{}, xerr.BadRequest(
			"flow: local @require " + importPath + ": " + err.Error())
	}

	rel, err := filepath.Rel(moduleRoot, abs)
	if err != nil {
		return Requirement{}, xerr.BadRequest(
			"flow: local @require " + importPath + ": " + err.Error())
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
			return "", "", xerr.NotFound(
				"no go.mod found above " + dir +
					" (local @require must point into a Go module)")
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

	return "", xerr.BadRequest(gomod + " has no module directive")
}

func looksLikeLocalPath(s string) bool {
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, `.\`) ||
		strings.HasPrefix(s, `..\`)
}

func appendUniqueRequirement(dst []Requirement, r Requirement) []Requirement {
	for _, existing := range dst {
		if existing.Import != r.Import {
			continue
		}

		if existing.Version != r.Version && existing.Version != "" && r.Version != "" {
			return append(dst, r)
		}

		return dst
	}

	return append(dst, r)
}
