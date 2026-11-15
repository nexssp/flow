package builder

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
)

// packageBuildSources snapshots the inputs used by the existing directive
// lifecycle, rather than flattening executable source or interpreting @include.
// The generated binary installs a SourceMap, so no packaged reference can fall
// back to a source file that happens to exist on the runtime machine.
func packageBuildSources(ctx context.Context, path, source string, requirements []require.Requirement) (inventory []compiler.BuildSource, snapshots map[string]string, err error) {
	files := make(map[string]string)
	var snapshotErr error
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	ctx = compiler.WithPreprocessObserver(ctx, func(event compiler.PreprocessEvent) {
		name, nameErr := relativeBuildSourceName(root, event.File)
		if nameErr != nil {
			snapshotErr = xerr.Conflict("build source path cannot be made relative: " + event.File)
			return
		}
		source, includeErr := rewriteAbsoluteIncludePaths(event.Original, root, event.Metadata)
		if includeErr != nil {
			snapshotErr = xerr.Conflict("build source include path cannot be made relative: " + event.File)
			return
		}
		content := rewriteEmbeddedRequires(source, filepath.Dir(event.File), requirements)
		if previous, exists := files[name]; exists && previous != content {
			snapshotErr = xerr.Conflict("build source changed between include evaluations: " + name)
			return
		}
		files[name] = content
	})
	sources, err := buildSourcesFrom(ctx, path, source)
	if err != nil {
		return nil, nil, err
	}
	if snapshotErr != nil {
		return nil, nil, snapshotErr
	}
	return sources, files, nil
}

func relativeBuildSourceName(root, path string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(absoluteRoot, absolute)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(relative)), nil
}

func rewriteAbsoluteIncludePaths(source, root string, metadata map[string]any) (string, error) {
	type replacement struct {
		start int
		end   int
		path  string
	}
	var replacements []replacement
	for _, imported := range compiler.SourceImports(metadata) {
		span := imported.TargetSpan
		if span.Start < 0 || span.End <= span.Start || span.End > len(source) {
			continue
		}
		target := source[span.Start:span.End]
		if !filepath.IsAbs(target) {
			continue
		}
		absoluteTarget, err := filepath.Abs(target)
		if err != nil {
			return "", err
		}
		relativeTarget, err := filepath.Rel(root, absoluteTarget)
		if err != nil {
			return "", err
		}
		replacements = append(replacements, replacement{
			start: span.Start,
			end:   span.End,
			path:  filepath.ToSlash(filepath.Clean(relativeTarget)),
		})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, item := range replacements {
		source = source[:item.start] + item.path + source[item.end:]
	}
	return source, nil
}
