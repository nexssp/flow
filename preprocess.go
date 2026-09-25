package flow

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/xerr"
)

type Preprocessed = directives.Preprocessed
type Pipeline = directives.Pipeline
type ActionMeta = directives.ActionMeta
type Requirement = directives.Requirement

func Preprocess(path string) (*Preprocessed, error) {
	visited := make(map[string]bool)
	return preprocessFile(path, visited)
}

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

// PreprocessFS pozwala na przetwarzanie manifestów z wirtualnego systemu plików (np. embed.FS)
// ze wsparciem dla dyrektywy @include.
func PreprocessFS(fsys fs.FS, path string) (*Preprocessed, error) {
	visited := make(map[string]bool)
	return preprocessFSFile(fsys, path, visited)
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

	resolver := func(p string) (*Preprocessed, error) {
		return preprocessFile(p, visited)
	}

	pre, err := preprocessSource(string(data), filepath.Dir(abs), abs, resolver)
	if err != nil {
		return nil, err
	}

	pre.Includes = append([]string{abs}, pre.Includes...)
	return pre, nil
}

func preprocessFSFile(fsys fs.FS, path string, visited map[string]bool) (*Preprocessed, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	if visited[cleanPath] {
		return nil, xerr.Conflict("flow: @include fs cycle: " + cleanPath)
	}
	visited[cleanPath] = true
	defer delete(visited, cleanPath)

	data, err := fs.ReadFile(fsys, cleanPath)
	if err != nil {
		return nil, xerr.NotFound("flow: read fs "+cleanPath, err)
	}

	baseDir := filepath.ToSlash(filepath.Dir(cleanPath))
	if baseDir == "." {
		baseDir = ""
	}

	resolver := func(p string) (*Preprocessed, error) {
		// embed.FS akceptuje tylko forward-slashe, mapujemy to by działało na Windowsie
		if !filepath.IsAbs(p) && baseDir != "" {
			p = filepath.ToSlash(filepath.Join(baseDir, p))
		}
		return preprocessFSFile(fsys, p, visited)
	}

	pre, err := preprocessSource(string(data), baseDir, cleanPath, resolver)
	if err != nil {
		return nil, err
	}

	pre.Includes = append([]string{cleanPath}, pre.Includes...)
	return pre, nil
}

func preprocessSource(src, baseDir, file string, resolver func(string) (*Preprocessed, error)) (*Preprocessed, error) {
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
		IncludeResolver: resolver,
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
