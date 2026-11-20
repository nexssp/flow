package embed

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xfile"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	descriptionext "github.com/nexssp/flow/extensions/description"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

//go:embed nflows
var hostFixtures embed.FS

func TestHostRootNFlowFixtureCompilesWithoutExecution(t *testing.T) {
	fixtures, err := fs.Sub(hostFixtures, "nflows")
	ktest.RequireNoError(t, err)
	source, err := fs.ReadFile(fixtures, "host_root.nflow")
	ktest.RequireNoError(t, err)
	terms, err := fs.ReadFile(fixtures, "host_root--terms.txt")
	ktest.RequireNoError(t, err)
	root := fstest.MapFS{"terms.txt": &fstest.MapFile{Data: terms}}

	embedBundle, _ := BundleWithFileResolver(root, "test.static", 128)
	cfg, err := runner.BuildConfig([]compiler.Bundle{embedBundle, descriptionext.Bundle(nil), runtimeext.Bundle(nil)})
	ktest.RequireNoError(t, err)
	_, err = runner.Compile(t.Context(), cfg, string(source), "host_root.nflow")
	ktest.RequireNoError(t, err)
}

func TestHostRootNFlowFixture(t *testing.T) {
	fixtures, err := fs.Sub(hostFixtures, "nflows")
	ktest.RequireNoError(t, err)
	source, err := fs.ReadFile(fixtures, "host_root.nflow")
	ktest.RequireNoError(t, err)
	terms, err := fs.ReadFile(fixtures, "host_root--terms.txt")
	ktest.RequireNoError(t, err)
	root := fstest.MapFS{"terms.txt": &fstest.MapFile{Data: terms}}

	embedBundle, _ := BundleWithFileResolver(root, "test.static", 128)
	cfg, err := runner.BuildConfig([]compiler.Bundle{embedBundle, descriptionext.Bundle(nil), runtimeext.Bundle(nil)})
	ktest.RequireNoError(t, err)
	compiled, err := runner.Compile(t.Context(), cfg, string(source), "host_root.nflow")
	ktest.RequireNoError(t, err)
	output, err := compiled.Program.DoAny(t.Context(), nil)
	ktest.RequireNoError(t, err)
	payload, ok := output.(map[string]any)
	ktest.RequireCondition(t, ok, "output = %#v, want a named argument payload", output)
	ref, ok := payload["value"].(xfile.Ref)
	ktest.RequireCondition(t, ok, "payload value = %#v, want a typed asset reference", payload["value"])
	ktest.RequireEqual(t, string(ref.Provider), "test.static")
	ktest.RequireEqual(t, int64(ref.ExpectedSize), int64(len(terms)))
	digest := sha256.Sum256(terms)
	ktest.RequireEqual(t, string(ref.ExpectedDigest), "sha256:"+hex.EncodeToString(digest[:]))
}

func TestEmbeddedTemplateNFlowFixture(t *testing.T) {
	fixtures, err := fs.Sub(hostFixtures, "nflows")
	ktest.RequireNoError(t, err)
	source, err := fs.ReadFile(fixtures, "template_render.nflow")
	ktest.RequireNoError(t, err)
	asset, err := fs.ReadFile(fixtures, "template_render--welcome.tmpl")
	ktest.RequireNoError(t, err)

	embedBundle, _ := BundleWithFileResolver(fstest.MapFS{
		"welcome.tmpl": &fstest.MapFile{Data: asset},
	}, "test.static", 128)
	bundles := append(native.SelftestBundles(), embedBundle)
	cfg, err := runner.BuildConfig(bundles)
	ktest.RequireNoError(t, err)
	ctx := xctx.WithUserID(t.Context(), "template-test-user")
	ctx = xctx.WithTenantID(ctx, "template-test-tenant")
	result, err := runner.Execute(ctx, cfg, string(source), "template_render.nflow", nil)
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, result.Output, "Welcome Ada\n")
}
