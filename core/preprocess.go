package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// SourceLoader resolves a directive-referenced source fragment such as
// the target of @include. CurrentFile is the path of the file that
// issued the reference; Target is the raw path from the directive.
//
// The returned resolvedPath is used for cycle detection and error
// reporting. Its only requirement is stability for the same file
// across calls within one preprocess run.
type SourceLoader interface {
	Load(currentFile, target string) (data []byte, resolvedPath string, err error)
}

type sourceLoaderContextKey struct{}

// WithSourceLoader installs a loader for directive-referenced sources.
// The default loader reads from the filesystem.
func WithSourceLoader(ctx context.Context, loader SourceLoader) context.Context {
	if loader == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceLoaderContextKey{}, loader)
}

func sourceLoaderFrom(ctx context.Context) SourceLoader {
	if loader, ok := ctx.Value(sourceLoaderContextKey{}).(SourceLoader); ok {
		return loader
	}
	return fileSystemLoader{}
}

type fileSystemLoader struct{}

func (fileSystemLoader) Load(currentFile, target string) (data []byte, resolvedPath string, err error) {
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(currentFile), target)
	}
	var absPath string
	absPath, err = filepath.Abs(resolved)
	if err != nil {
		return nil, "", err
	}
	data, err = os.ReadFile(absPath)
	if err != nil {
		return nil, "", err
	}
	return data, absPath, nil
}

// Preprocess executes directives from the dt table.
//
// Cycle detection uses a call-stack, not a global set. This allows
// diamond dependencies (A → B → common, A → C → common) while still
// catching true cycles (A → B → A). The error message carries the
// exact include chain so the user sees which edge closes the loop.
//
// Source fragments referenced by directives are read through the
// loader installed with WithSourceLoader; the default reads from the
// filesystem.
func Preprocess(ctx context.Context, dt *DirectiveTable, source, file string) (clean string, meta map[string]any, err error) {
	meta = make(map[string]any)
	if dt == nil {
		return source, meta, nil
	}
	stack := &includeStack{seen: make(map[string]bool, 8)}
	if file != "" {
		stack.push(file)
	}
	return preprocess(ctx, dt, source, file, stack)
}

type includeStack struct {
	seen map[string]bool
	path []string
}

// push returns false when file is already on the current path.
func (s *includeStack) push(file string) bool {
	if s.seen[file] {
		return false
	}
	s.seen[file] = true
	s.path = append(s.path, file)
	return true
}

func (s *includeStack) pop() {
	if n := len(s.path); n > 0 {
		last := s.path[n-1]
		delete(s.seen, last)
		s.path = s.path[:n-1]
	}
}

func (s *includeStack) chain(extra string) string {
	parts := make([]string, len(s.path), len(s.path)+1)
	copy(parts, s.path)
	parts = append(parts, extra)
	return strings.Join(parts, " → ")
}

func preprocess(
	ctx context.Context,
	dt *DirectiveTable,
	source, file string,
	stack *includeStack,
) (clean string, meta map[string]any, err error) {
	meta = make(map[string]any)
	if dt == nil {
		return source, meta, nil
	}

	loader := sourceLoaderFrom(ctx)

	baseDir := ""
	if file != "" {
		baseDir = filepath.Dir(file)
	}

	lines := strings.Split(source, "\n")
	body := make([]string, len(lines))
	blanked := make(map[int]bool)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		if !strings.HasPrefix(line, "@") || strings.HasPrefix(line, "@{") {
			if !blanked[i] {
				body[i] = lines[i]
			}
			i++
			continue
		}

		name := directiveName(line)
		handler, ok := dt.ByName(name)
		if !ok {
			if !blanked[i] {
				body[i] = lines[i]
			}
			i++
			continue
		}

		req := DirectiveReq{
			Lines:   lines,
			Body:    body,
			I:       i,
			Out:     meta,
			File:    file,
			BaseDir: baseDir,
			Table:   dt,
			Recurse: func(target string) (string, map[string]any, error) {
				data, resolvedPath, loadErr := loader.Load(file, target)
				if loadErr != nil {
					return "", nil, loadErr
				}
				if !stack.push(resolvedPath) {
					return "", nil, SourceError(
						Position{File: file, Line: i + 1},
						"@include cycle detected: %s", stack.chain(resolvedPath))
				}
				defer stack.pop()
				return preprocess(ctx, dt, string(data), resolvedPath, stack)
			},
		}
		res, err := handler.Do(ctx, req)
		if err != nil {
			return "", nil, err
		}

		for _, bl := range res.BlankLines {
			if bl >= 0 && bl < len(body) {
				blanked[bl] = true
				body[bl] = ""
			}
		}

		next := res.Next
		if next <= i {
			next = i + 1
		}
		i = next
	}

	return strings.Join(body, "\n"), meta, nil
}

func directiveName(line string) string {
	if len(line) < 2 || line[0] != '@' {
		return ""
	}
	rest := line[1:]
	for i := range len(rest) {
		switch rest[i] {
		case ' ', '\t', ':', '=', '{':
			return rest[:i]
		}
	}
	return rest
}
