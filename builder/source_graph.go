package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
)

// harnessSourceGraph supplies the native-bundle scanner with declarations and
// action references from every included source, not just the entry point.
// Keep the raw text: preprocessing removes directives and pipeline bodies that
// still determine which packages the generated runner must link.
func harnessSourceGraph(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return harnessSourceGraphFrom(ctx, path, string(data))
}

func harnessSourceGraphFrom(ctx context.Context, path, source string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	loader := &harnessSourceLoader{ctx: ctx}
	loader.source.WriteString(source)
	loader.source.WriteByte('\n')
	// Let the shipped include directive resolve paths and the preprocessor
	// handle constants, nested includes and cycle detection. Do not parse
	// @include lines independently or flatten the executable workflow here.
	clean, _, err := compiler.Preprocess(compiler.WithSourceLoader(ctx, loader), native.Directives(), source, path)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	loader.source.WriteString(clean)
	loader.source.WriteByte('\n')
	return loader.source.String(), nil
}

type harnessSourceLoader struct {
	ctx    context.Context
	source strings.Builder
}

func (l *harnessSourceLoader) Load(currentFile, target string) (data []byte, resolvedPath string, err error) {
	if contextErr := l.ctx.Err(); contextErr != nil {
		return nil, "", contextErr
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(currentFile), target)
	}
	path, err := filepath.Abs(target)
	if err != nil {
		return nil, "", err
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	l.source.Write(data)
	l.source.WriteByte('\n')
	return data, path, nil
}
