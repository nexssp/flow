package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"

	"github.com/nexssp/flow/compiler"
)

func TestEmbedDirectiveCompilesTypedRefWithoutAssetBytesInMeta(t *testing.T) {
	asset := []byte("vector-logo")
	bundle := NewBundle(fstest.MapFS{
		"assets/logo.svg": &fstest.MapFile{Data: asset},
	}, "test.static", 128)
	dt := compiler.NewDirectiveTable(bundle.Directives...)
	_, meta, err := compiler.Preprocess(t.Context(), dt, "@embed logo \"assets/logo.svg\"\ntest.noop @{ value: embed.logo }", "test.nflow")
	if err != nil {
		t.Fatal(err)
	}
	refs, ok := meta[refsMetaKey].(map[string]xfile.Ref)
	if !ok || len(refs) != 1 {
		t.Fatalf("metadata embeds = %#v, want one typed reference", meta[refsMetaKey])
	}
	digest := sha256.Sum256(asset)
	wantDigest := "sha256:" + hex.EncodeToString(digest[:])
	ref := refs["logo"]
	if string(ref.Provider) != "test.static" || len(ref.ID) != 64 || strings.Contains(string(ref.ID), "/") || string(ref.Version) != wantDigest || string(ref.ExpectedDigest) != wantDigest || int64(ref.ExpectedSize) != int64(len(asset)) {
		t.Fatalf("compiled ref = %#v", ref)
	}
	if symbols := compiler.SymbolsFromMeta(meta); symbols["embed"].Symbols["logo"] != ref {
		t.Fatalf("embed symbol namespace = %#v", symbols)
	}
	if strings.Contains(fmt.Sprintf("%#v", meta), string(asset)) {
		t.Fatal("asset bytes leaked into metadata")
	}
}

func TestResolverRequiresScopeAndRechecksDeclaredAsset(t *testing.T) {
	assets := fstest.MapFS{"assets/logo.svg": &fstest.MapFile{Data: []byte("original")}}
	bundle, resolver := BundleWithFileResolver(assets, "test.static", 128)
	_, meta, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(bundle.Directives...), `@embed logo "assets/logo.svg"`, "test.nflow")
	if err != nil {
		t.Fatal(err)
	}
	refs, ok := meta[refsMetaKey].(map[string]xfile.Ref)
	if !ok {
		t.Fatalf("metadata embeds = %#v, want typed references", meta[refsMetaKey])
	}
	ref := refs["logo"]
	if _, _, err := resolver.Open(t.Context(), ref); xerr.KindFrom(err) != xerr.KindUnauthorized {
		t.Fatalf("open without caller/tenant scope = %v, want Unauthorized", err)
	}

	ctx := xctx.WithUserID(t.Context(), "test-caller")
	ctx = xctx.WithTenantID(ctx, "test-tenant")
	assets["assets/logo.svg"] = &fstest.MapFile{Data: []byte("tampered")}
	if _, _, err := resolver.Open(ctx, ref); xerr.KindFrom(err) != xerr.KindConflict {
		t.Fatalf("open changed asset = %v, want Conflict", err)
	}

	forged := xfile.Ref{Provider: ref.Provider, ID: xfile.ID("assets/logo.svg")}
	if _, _, err := resolver.Open(ctx, forged); xerr.KindFrom(err) != xerr.KindNotFound {
		t.Fatalf("open path-like undeclared ID = %v, want NotFound", err)
	}
}

func TestEmbedDirectiveRejectsInvalidDeclarations(t *testing.T) {
	bundle := NewBundle(fstest.MapFS{
		"ok.txt":  &fstest.MapFile{Data: []byte("ok")},
		"big.txt": &fstest.MapFile{Data: []byte("big")},
	}, "test.static", 2)
	dt := compiler.NewDirectiveTable(bundle.Directives...)
	cases := []struct {
		name string
		src  string
	}{
		{"traversal", "@embed secret \"../outside.txt\""},
		{"missing", "@embed missing \"missing.txt\""},
		{"oversize", "@embed big \"big.txt\""},
		{"syntax", "@embed \"ok.txt\""},
		{"duplicate", "@embed item \"ok.txt\"\n@embed item \"ok.txt\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := compiler.Preprocess(t.Context(), dt, tc.src, "test.nflow"); err == nil {
				t.Fatal("invalid declaration unexpectedly succeeded")
			}
		})
	}
}

func TestEmbedRequiresDeclaredBundleRoot(t *testing.T) {
	bundle := NewBundle(nil, "test.static", 32)
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(bundle.Directives...), "@embed item \"ok.txt\"", "test.nflow")
	if err == nil || !strings.Contains(err.Error(), "declared Bundle fs.FS/root") {
		t.Fatalf("error = %v, want missing declared root", err)
	}
}

func TestRootBundleRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	bundle := Bundle(map[string]string{"root": root})
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(bundle.Directives...), "@embed secret \"escape.txt\"", "test.nflow")
	if err == nil {
		t.Fatal("escaping symlink unexpectedly succeeded")
	}
}

func TestUnknownEmbedNameFailsCompile(t *testing.T) {
	bundle := NewBundle(fstest.MapFS{"ok.txt": &fstest.MapFile{Data: []byte("ok")}}, "test.static", 32)
	act := action.New("test.noop", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := compiler.NewDynamicResolver(action.Library{Name: "test", Actions: []action.AnyAction{act}})
	if err != nil {
		t.Fatal(err)
	}
	compile := compiler.CompileAction(
		resolver,
		compiler.NewDirectiveTable(bundle.Directives...),
		compiler.NewModifierTable(),
		compiler.NewOperatorTable(),
		compiler.DefaultPrimaryExtensions(),
		compiler.BundleConfig(bundle)...,
	)
	_, err = compile.Do(t.Context(), compiler.CompileReq{Source: "@embed declared \"ok.txt\"\ntest.noop @{ value: embed.missing }", Name: "test.nflow"})
	if err == nil || !strings.Contains(err.Error(), "unknown embed.missing") {
		t.Fatalf("compile error = %v, want unknown embed name", err)
	}
}

func TestUnknownEmbedNameFailsCompileWithoutDeclaration(t *testing.T) {
	bundle := NewBundle(fstest.MapFS{}, "test.static", 32)
	act := action.New("test.noop", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := compiler.NewDynamicResolver(action.Library{Name: "test", Actions: []action.AnyAction{act}})
	if err != nil {
		t.Fatal(err)
	}
	compile := compiler.CompileAction(
		resolver,
		compiler.NewDirectiveTable(bundle.Directives...),
		compiler.NewModifierTable(),
		compiler.NewOperatorTable(),
		compiler.DefaultPrimaryExtensions(),
		compiler.BundleConfig(bundle)...,
	)
	_, err = compile.Do(t.Context(), compiler.CompileReq{Source: "test.noop @{ value: embed.missing }", Name: "test.nflow"})
	if err == nil || !strings.Contains(err.Error(), "unknown embed.missing") {
		t.Fatalf("compile error = %v, want unknown embed name", err)
	}
}

func TestKnownEmbedCannotBePassedAsLegacyStringParameter(t *testing.T) {
	bundle := NewBundle(fstest.MapFS{"ok.txt": &fstest.MapFile{Data: []byte("ok")}}, "test.static", 32)
	act := action.New("test.noop", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := compiler.NewDynamicResolver(action.Library{Name: "test", Actions: []action.AnyAction{act}})
	if err != nil {
		t.Fatal(err)
	}
	compile := compiler.CompileAction(
		resolver,
		compiler.NewDirectiveTable(bundle.Directives...),
		compiler.NewModifierTable(),
		compiler.NewOperatorTable(),
		compiler.DefaultPrimaryExtensions(),
		compiler.BundleConfig(bundle)...,
	)
	_, err = compile.Do(t.Context(), compiler.CompileReq{Source: "@embed declared \"ok.txt\"\ntest.noop(value=embed.declared)", Name: "test.nflow"})
	if err == nil || !strings.Contains(err.Error(), "typed @{ ... } value") {
		t.Fatalf("compile error = %v, want typed-value guidance", err)
	}
}

func TestKnownEmbedCompilesToTypedRef(t *testing.T) {
	bundle := NewBundle(fstest.MapFS{"ok.txt": &fstest.MapFile{Data: []byte("ok")}}, "test.static", 32)
	act := action.New("test.noop", func(_ context.Context, in any) (any, error) { return in, nil }).Build()
	resolver, err := compiler.NewDynamicResolver(action.Library{Name: "test", Actions: []action.AnyAction{act}})
	if err != nil {
		t.Fatal(err)
	}
	compile := compiler.CompileAction(
		resolver,
		compiler.NewDirectiveTable(bundle.Directives...),
		compiler.NewModifierTable(),
		compiler.NewOperatorTable(),
		compiler.DefaultPrimaryExtensions(),
		compiler.BundleConfig(bundle)...,
	)
	result, err := compile.Do(t.Context(), compiler.CompileReq{Source: "@embed declared \"ok.txt\"\ntest.noop @{ value: embed.declared }", Name: "test.nflow"})
	if err != nil {
		t.Fatal(err)
	}
	atom, ok := result.AST.(*compiler.Atom)
	if !ok {
		t.Fatalf("AST = %T, want atom", result.AST)
	}
	value := atom.Args["value"]
	if value == nil || value.Kind != compiler.ValueTyped {
		t.Fatalf("compiled argument = %#v, want ValueTyped", value)
	}
	ref, ok := value.Typed.(xfile.Ref)
	if !ok || len(ref.ID) != 64 {
		t.Fatalf("typed argument = %#v, want xfile.Ref", value.Typed)
	}
	output, err := result.Program.DoAny(t.Context(), nil)
	if err != nil {
		t.Fatalf("run compiled program: %v", err)
	}
	payload, ok := output.(map[string]any)
	if !ok {
		t.Fatalf("runtime payload = %T, want map[string]any", output)
	}
	if got, ok := payload["value"].(xfile.Ref); !ok || got != ref {
		t.Fatalf("runtime value = %#v, want typed xfile.Ref %#v", payload["value"], ref)
	}
}

func TestExpressionValidatorIgnoresQuotedTextButRejectsUnknown(t *testing.T) {
	refs := map[string]xfile.Ref{"known": {Provider: "test", ID: "known", Version: "v1"}}
	meta := map[string]any{refsMetaKey: refs, "asserts": []string{
		`value == "embed.not-a-ref"`,
		`value == 'embed.not-a-ref'`,
		"value == `embed.not-a-ref`",
		"value == true // embed.not-a-ref",
		"# embed.not-a-ref",
	}}
	if err := validateRefs(nil, meta); err != nil {
		t.Fatalf("quoted or commented text rejected: %v", err)
	}
	meta["asserts"] = []string{"embed.unknown != nil"}
	if err := validateRefs(nil, meta); err == nil || !strings.Contains(err.Error(), "unknown embed.unknown") {
		t.Fatalf("unknown expression ref error = %v", err)
	}
	meta["asserts"] = nil
	if err := validateRefs(&compiler.LoopExpr{Until: "embed.loop_missing == true"}, meta); err == nil || !strings.Contains(err.Error(), "unknown embed.loop_missing") {
		t.Fatalf("unknown loop-condition ref error = %v", err)
	}
	if err := validateRefs(&compiler.LoopExpr{Until: "embed.known == true"}, meta); err == nil || !strings.Contains(err.Error(), "typed @{ ... } value") {
		t.Fatalf("known loop-condition ref error = %v, want typed-value guidance", err)
	}
}

func TestValidateValueIgnoresNonReferenceKinds(t *testing.T) {
	kinds := []struct {
		name string
		kind compiler.ValueKind
	}{
		{"string", compiler.ValueString},
		{"number", compiler.ValueNumber},
		{"bool", compiler.ValueBool},
		{"null", compiler.ValueNull},
		{"ref", compiler.ValueRef},
		{"typed", compiler.ValueTyped},
	}
	for _, test := range kinds {
		t.Run(test.name, func(t *testing.T) {
			called := false
			err := validateValue(&compiler.Value{Kind: test.kind, Str: "embed.missing"}, func(string) error {
				called = true
				return nil
			})
			if err != nil {
				t.Fatalf("validateValue() error = %v", err)
			}
			if called {
				t.Fatal("non-reference value was treated as an embed reference")
			}
		})
	}
}

func TestMakeRefReturnsFileCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	fsys := closeErrorFS{
		inner: fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("content")}},
		err:   closeErr,
	}
	if _, err := makeRef(fsys, "test.static", "file.txt", 128); !errors.Is(err, closeErr) {
		t.Fatalf("makeRef() error = %v, want close error", err)
	}
}

type closeErrorFS struct {
	inner fs.FS
	err   error
}

func (fsys closeErrorFS) Open(name string) (fs.File, error) {
	file, err := fsys.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return closeErrorFile{File: file, err: fsys.err}, nil
}

type closeErrorFile struct {
	fs.File
	err error
}

func (file closeErrorFile) Close() error { return file.err }

func TestBundleMaxSizeOptionMustBePositive(t *testing.T) {
	bundle := Bundle(map[string]string{"max_size": "0"})
	_, _, err := compiler.Preprocess(t.Context(), compiler.NewDirectiveTable(bundle.Directives...), "@embed name \"x\"", "test.nflow")
	if err == nil || !strings.Contains(err.Error(), "max_size must be a positive integer") {
		t.Fatalf("error = %v", err)
	}
}
