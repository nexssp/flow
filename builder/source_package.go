package builder

import (
	"context"
	"path/filepath"

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
	ctx = compiler.WithPreprocessObserver(ctx, func(event compiler.PreprocessEvent) {
		name := filepath.Clean(event.File)
		content := rewriteEmbeddedRequires(event.Original, filepath.Dir(name), requirements)
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
