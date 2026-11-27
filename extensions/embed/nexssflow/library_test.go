package nexssflow

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	embedext "github.com/nexssp/flow/extensions/embed"
	"github.com/nexssp/flow/extensions/require"
)

func TestBundleExposesRegisteredEmbedExtension(t *testing.T) {
	bundle := Bundle(nil)
	ktest.RequireEqual(t, bundle.ID, embedext.ID)
	ktest.RequireCondition(t, len(bundle.Directives) == 1 && bundle.Directives[0].Name == "embed", "bundle directives = %#v, want @embed", bundle.Directives)

	_, ok := compiler.Lookup(embedext.ID)
	ktest.RequireCondition(t, ok, "embed bundle factory was not registered")
}

func TestBundleRequireOptionValidation(t *testing.T) {
	t.Run("accepts supported options", func(t *testing.T) {
		_, err := require.ResolveBundles([]require.Requirement{{
			Import: embedext.ID,
			Options: map[string]string{
				"root":           "",
				"provider":       embedext.DefaultProvider,
				"max_size":       "1024",
				"max_total_size": "2048",
				"max_files":      "2",
			},
		}})
		if err != nil {
			t.Fatalf("resolve supported embed options: %v", err)
		}
	})

	t.Run("rejects unknown options", func(t *testing.T) {
		_, err := require.ResolveBundles([]require.Requirement{{
			Import:  embedext.ID,
			Options: map[string]string{"max_szie": "1024"},
		}})
		ktest.RequireCondition(t, err != nil, "expected misspelled embed option to be rejected")
		ktest.RequireStringContains(t, err.Error(), `unknown option "max_szie"`)
	})
}
