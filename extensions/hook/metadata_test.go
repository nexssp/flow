package hook

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerAppendsHooks(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"hooks": []string{"parent"}}
	included := map[string]any{"hooks": []string{"included"}}

	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	ktest.RequireEqual(t, parent["hooks"], any([]string{"parent", "included"}))
}

func TestMetadataMergerRejectsWrongType(t *testing.T) {
	t.Parallel()
	parent := map[string]any{}
	included := map[string]any{"hooks": "not a slice"}
	ktest.RequireErrorContains(t, Directive.Metadata.Merge(parent, included), "expected []string")
}
