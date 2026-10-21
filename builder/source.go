package builder

import (
	"context"
	"os"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

// SourceRequires resolves @require declarations using the shipped native directives.
func SourceRequires(ctx context.Context, path string) ([]require.Requirement, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, meta, err := compiler.Preprocess(ctx, native.Directives(), string(source), path)
	if err != nil {
		return nil, err
	}
	return require.SourceRequirements(meta)
}

func hasRequirement(reqs []require.Requirement, id string) bool {
	for i := range reqs {
		if reqs[i].Import == id || require.NormalizeID(reqs[i].Import) == id {
			return true
		}
	}
	return false
}
