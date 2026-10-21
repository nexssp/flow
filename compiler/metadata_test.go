package compiler

import (
	"errors"
	"slices"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func requireMetadataStrings(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	got, ok := metadata[key].([]string)
	ktest.RequireCondition(t, ok, "metadata %q has type %T, want []string", key, metadata[key])
	ktest.RequireCondition(t, slices.Equal(got, want), "metadata %q = %v, want %v", key, got, want)
}

func TestAppendMetadata(t *testing.T) {
	backing := make([]string, 4)
	parentItems := backing[:1]
	parentItems[0] = "parent"
	parent := map[string]any{"items": parentItems}
	includedItems := []string{"included"}
	included := map[string]any{"items": includedItems}
	table := NewDirectiveTable(Directive{Name: "append", Metadata: AppendMetadata[string]("items")})

	ktest.RequireNoError(t, table.MergeMetadata(DirectiveReq{Out: parent}, included))
	requireMetadataStrings(t, parent, "items", []string{"parent", "included"})
	ktest.RequireEqual(t, backing[:2], []string{"parent", ""})
	ktest.RequireEqual(t, includedItems, []string{"included"})
}

func TestAppendMetadataTypeMismatch(t *testing.T) {
	parent := map[string]any{"items": []string{"parent"}}
	table := NewDirectiveTable(Directive{Name: "append", Metadata: AppendMetadata[string]("items")})

	err := table.MergeMetadata(DirectiveReq{Out: parent}, map[string]any{"items": 42})
	ktest.RequireErrorContains(t, err, `metadata "items"`)
	ktest.RequireErrorContains(t, err, "int")
	requireMetadataStrings(t, parent, "items", []string{"parent"})
}

func TestNewDirectiveTableRejectsDuplicateMetadataKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate metadata key to panic")
		}
	}()
	NewDirectiveTable(
		Directive{Name: "first", Metadata: AppendMetadata[string]("shared")},
		Directive{Name: "second", Metadata: AppendMetadata[string]("shared")},
	)
}

func TestMergeMetadataUnknownKeysUseParentWins(t *testing.T) {
	parent := map[string]any{"shared": "parent", "parent-only": 1}
	table := NewDirectiveTable(Directive{Name: "custom", Metadata: AppendMetadata[string]("managed")})

	ktest.RequireNoError(t, table.MergeMetadata(DirectiveReq{Out: parent}, map[string]any{
		"shared":        "included",
		"included-only": 2,
	}))
	ktest.RequireEqual(t, parent["shared"], "parent")
	ktest.RequireEqual(t, parent["parent-only"], 1)
	ktest.RequireEqual(t, parent["included-only"], 2)
}

func TestMergeMetadataErrorDoesNotCommitMap(t *testing.T) {
	wantErr := errors.New("merge failed")
	parent := map[string]any{"value": "original"}
	table := NewDirectiveTable(Directive{Name: "custom", Metadata: &MetadataMerger{
		Keys: []string{"value"},
		Merge: func(working, _ map[string]any) error {
			working["value"] = "temporary"
			return wantErr
		},
	}})

	err := table.MergeMetadata(DirectiveReq{Out: parent}, map[string]any{"value": "included"})
	ktest.RequireEqual(t, err, wantErr)
	ktest.RequireEqual(t, parent["value"], "original")
}

func TestMergeMetadataAfterMergeCalledOnce(t *testing.T) {
	afterCalls := 0
	parent := map[string]any{"items": []string{"parent"}}
	table := NewDirectiveTable(Directive{Name: "append", Metadata: &MetadataMerger{
		Keys:  []string{"items"},
		Merge: AppendMetadata[string]("items").Merge,
		AfterMerge: func(req DirectiveReq) error {
			afterCalls++
			requireMetadataStrings(t, req.Out, "items", []string{"parent", "included"})
			requireMetadataStrings(t, parent, "items", []string{"parent"})
			return nil
		},
	}})

	ktest.RequireNoError(t, table.MergeMetadata(DirectiveReq{Out: parent}, map[string]any{"items": []string{"included"}}))
	ktest.RequireEqual(t, afterCalls, 1)
	requireMetadataStrings(t, parent, "items", []string{"parent", "included"})
}

func TestMergeMetadataNilTableUsesParentWinsDefaults(t *testing.T) {
	parent := map[string]any{"shared": "parent"}
	ktest.RequireNoError(t, (*DirectiveTable)(nil).MergeMetadata(DirectiveReq{Out: parent}, map[string]any{
		"shared": "included",
		"new":    true,
	}))
	ktest.RequireEqual(t, parent["shared"], "parent")
	ktest.RequireEqual(t, parent["new"], true)
}
