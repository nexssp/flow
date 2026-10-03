package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Preprocess executes directives from the dt table.
//
// Cycle detection uses a call-stack, not a global set. This allows
// diamond dependencies (A → B → common, A → C → common) while still
// catching true cycles (A → B → A). The error message carries the
// exact include chain so the user sees which edge closes the loop.
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
			Recurse: func(absPath string) (string, map[string]any, error) {
				if !stack.push(absPath) {
					return "", nil, SourceError(
						Position{File: file, Line: i + 1},
						"@include cycle detected: %s", stack.chain(absPath))
				}
				defer stack.pop()

				data, readErr := os.ReadFile(absPath)
				if readErr != nil {
					return "", nil, readErr
				}
				return preprocess(ctx, dt, string(data), absPath, stack)
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
