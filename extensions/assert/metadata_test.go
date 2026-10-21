package assert

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerAppendsAssertionsCopyOnWrite(t *testing.T) {
	t.Parallel()
	backing := make([]string, 4)
	parentSlice := backing[:1]
	parentSlice[0] = "parent"
	includedSlice := []string{"included"}
	parent := map[string]any{"asserts": parentSlice}
	included := map[string]any{"asserts": includedSlice}

	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	ktest.RequireEqual(t, parent["asserts"], any([]string{"parent", "included"}))
	ktest.RequireEqual(t, backing[:2], []string{"parent", ""})
	ktest.RequireEqual(t, includedSlice, []string{"included"})
}
