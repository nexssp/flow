package pool

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerRejectsDuplicatePoolName(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"pools": []Declaration{{Name: "fast"}}}
	included := map[string]any{"pools": []Declaration{{Name: "fast"}}}
	ktest.RequireErrorContains(t, Directive.Metadata.Merge(parent, included), "duplicate declaration")
	ktest.RequireEqual(t, len(DeclarationsFromMeta(parent)), 1)
}

func TestMetadataMergerAppendsPools(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"pools": []Declaration{{Name: "fast"}}}
	included := map[string]any{"pools": []Declaration{{Name: "safe"}}}
	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	ktest.RequireEqual(t, []string{DeclarationsFromMeta(parent)[0].Name, DeclarationsFromMeta(parent)[1].Name}, []string{"fast", "safe"})
	ktest.RequireEqual(t, len(DeclarationsFromMeta(included)), 1)
}
