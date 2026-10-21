package builder

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

// buildSources collects the existing preprocessor's lifecycle. It neither
// replaces SourceLoader nor parses include paths, directives or metadata.
func buildSources(ctx context.Context, path string) ([]compiler.BuildSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return buildSourcesFrom(ctx, path, string(data))
}

func buildSourcesFrom(ctx context.Context, path, source string) ([]compiler.BuildSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var sources []compiler.BuildSource
	ctx = compiler.WithPreprocessObserver(ctx, func(event compiler.PreprocessEvent) {
		sources = append(sources, compiler.BuildSource{
			File: event.File, Raw: event.Source, Clean: event.Clean,
			Metadata: event.Metadata, Directives: event.Directives,
		})
	})
	_, _, err = compiler.Preprocess(ctx, native.Directives(), source, path)
	if err != nil {
		return nil, err
	}
	return sources, nil
}

func nativeBundlesForSources(sources []compiler.BuildSource, requirements []require.Requirement, all bool) ([]nativeBundleInfo, error) {
	plan, err := native.PlanBuild(sources, requirements, all)
	if err != nil {
		return nil, err
	}
	bundles := make([]nativeBundleInfo, 0, len(plan))
	for _, entry := range plan {
		bundles = append(bundles, nativeBundleInfo{id: entry.ID, pkg: entry.ImportPath})
	}
	return bundles, nil
}
