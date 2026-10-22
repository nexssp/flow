package embed

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/nexssp/kernel/xfile"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	descriptionext "github.com/nexssp/flow/extensions/description"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/runner"
)

//go:embed nflows
var hostFixtures embed.FS

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
	ref, ok := output.(xfile.Ref)
	ktest.RequireCondition(t, ok, "output = %#v, want a typed asset reference", output)
	ktest.RequireEqual(t, string(ref.Provider), "test.static")
	ktest.RequireEqual(t, int64(ref.ExpectedSize), int64(len(terms)))
	digest := sha256.Sum256(terms)
	ktest.RequireEqual(t, string(ref.ExpectedDigest), "sha256:"+hex.EncodeToString(digest[:]))
}
