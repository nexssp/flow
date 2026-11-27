package nexssflow

import (
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	embedext "github.com/nexssp/flow/extensions/embed"
)

func TestBundleExposesRegisteredEmbedExtension(t *testing.T) {
	bundle := Bundle(nil)
	ktest.RequireEqual(t, bundle.ID, embedext.ID)
	ktest.RequireCondition(t, len(bundle.Directives) == 1 && bundle.Directives[0].Name == "embed", "bundle directives = %#v, want @embed", bundle.Directives)

	_, ok := compiler.Lookup(embedext.ID)
	ktest.RequireCondition(t, ok, "embed bundle factory was not registered")
}
