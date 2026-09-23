package flow

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/xerr"
)

// Preprocessed is the alias for directives.Preprocessed. Callers that
// import flow see flow.Preprocessed; the implementation lives in
// flow/directives so the directive registry and the data shape stay
// together.
type Preprocessed = directives.Preprocessed
type Pipeline = directives.Pipeline
type ActionMeta = directives.ActionMeta
type Requirement = directives.Requirement

// Preprocess reads a .nflow file from disk and resolves every
// directive: @profile, @include, @pipeline … @end, @action,
// @description, @require. The resulting DSL is line-aligned with the
// source file so parser errors point at the original line numbers.
func Preprocess(path string) (*Preprocessed, error) {
	visited := make(map[string]bool)
	return preprocessFile(path, visited)
}

// PreprocessBytes processes an in-memory .nflow source. It is the
// entry point for embedded libraries (go:embed) that carry their
// pipeline as a string rather than as a file on disk.
//
// @include is rejected in this mode: an embedded source has no on-disk
// base directory, so relative paths cannot be resolved.
func PreprocessBytes(source []byte, name string) (*Preprocessed, error) {
	if name == "" {
		name = "<embedded>"
	}

	pre, err := preprocessSource(string(source), "", name, nil)
	if err != nil {
		return nil, err
	}

	pre.Includes = append([]string{name}, pre.Includes...)
	return pre, nil
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

	pre, err := preprocessSource(string(data), filepath.Dir(abs), abs, visited)
	if err != nil {
		return nil, err
	}

	pre.Includes = append([]string{abs}, pre.Includes...)
	return pre, nil
}

// preprocessSource returns a Preprocessed whose DSL is line-aligned
// with the source: every input line produces exactly one output line.
// Directive lines are replaced by empty lines, and multi-line
// directives (@pipeline … @end) leave their slots empty, which keeps
// parser error positions pointing at the original file.
func preprocessSource(src, baseDir, file string, visited map[string]bool) (*Preprocessed, error) {
	out := &Preprocessed{
		File:         file,
		Config:       map[string]string{},
		Declarations: map[string]any{},
	}

	lines := strings.Split(src, "\n")
	body := make([]string, len(lines))

	ctx := &directives.Context{
		Out:     out,
		BaseDir: baseDir,
		File:    file,
		Body:    body,
		ValidateProfile: func(name string) error {
			_, err := LookupProfile(name)
			return err
		},
	}

	// The include resolver is only wired up in file mode. In byte mode
	// (PreprocessBytes) visited is nil and @include will fail with a
	// clear message from at_include.go.
	if visited != nil {
		ctx.IncludeResolver = func(p string) (*Preprocessed, error) {
			return preprocessFile(p, visited)
		}
	}

	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		if d, ok := directives.Lookup(trimmed); ok {
			next, err := d.Apply(ctx, lines, i)
			if err != nil {
				return nil, err
			}
			i = next
			continue
		}

		body[i] = lines[i]
		i++
	}

	out.DSL = strings.Join(body, "\n")
	return out, nil
}
