package schema

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestMetadataMergerRejectsDuplicateSchemaName(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"schemas": []Schema{{Name: "Plan"}}}
	included := map[string]any{"schemas": []Schema{{Name: "Plan"}}}
	ktest.RequireErrorContains(t, Directive.Metadata.Merge(parent, included), "duplicate declaration")
	ktest.RequireEqual(t, len(SchemasFromMap(parent)), 1)
}

func TestMetadataMergerAppendsSchemas(t *testing.T) {
	t.Parallel()
	parent := map[string]any{"schemas": []Schema{{Name: "Base", Fields: []Field{{Name: "ID"}}}}}
	included := map[string]any{"schemas": []Schema{{Name: "Plan", Fields: []Field{{Name: "Name"}}}}}
	ktest.RequireNoError(t, Directive.Metadata.Merge(parent, included))
	merged := SchemasFromMap(parent)
	ktest.RequireEqual(t, len(merged), 2)
	ktest.RequireEqual(t, merged[1].Fields[0].Name, "Name")
	ktest.RequireEqual(t, SchemasFromMap(included)[0].Fields[0].Name, "Name")
}
