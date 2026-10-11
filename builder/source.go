package builder

import (
	"context"
	"os"
	"strings"

	core "github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
)

// SourceRequires resolves @require declarations using the shipped native directives.
func SourceRequires(ctx context.Context, path string) ([]require.Requirement, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, meta, err := core.Preprocess(ctx, native.Directives(), string(source), path)
	if err != nil {
		return nil, err
	}
	reqs := require.FromMeta(meta)
	if strings.Contains(string(source), "cov.") && !hasRequirement(reqs, "selftestkit") {
		reqs = append(reqs, require.Requirement{Import: "selftestkit"})
	}
	return reqs, nil
}

func hasRequirement(reqs []require.Requirement, id string) bool {
	for i := range reqs {
		if reqs[i].Import == id || require.NormalizeID(reqs[i].Import) == id {
			return true
		}
	}
	return false
}
