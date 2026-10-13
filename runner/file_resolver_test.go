package runner

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
	embedext "github.com/nexssp/flow/extensions/embed"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
)

func TestBundleWithFileResolverStagesEmbedAndComposesStorage(t *testing.T) {
	asset := []byte("<svg>embedded asset</svg>")
	embedBundle, embedResolver := embedext.BundleWithFileResolver(fstest.MapFS{
		"assets/logo.svg": &fstest.MapFile{Data: asset},
	}, "flow.static", 1024)
	if embedResolver == nil {
		t.Fatal("BundleWithFileResolver returned a nil resolver")
	}

	storageRef := xfile.Ref{Provider: "object.store", ID: "opaque-object-id"}
	storageResolver := testFileResolver(func(_ context.Context, ref xfile.Ref) (io.ReadCloser, xfile.Info, error) {
		if ref != storageRef {
			t.Fatalf("storage ref = %#v, want %#v", ref, storageRef)
		}
		return io.NopCloser(strings.NewReader("storage asset")), xfile.Info{
			Provider: ref.Provider,
			ID:       ref.ID,
			Version:  "v1",
			Size:     int64(len("storage asset")),
			Digest:   "sha256:test",
		}, nil
	})
	storageBundle := compiler.BundleWithFileResolver(compiler.Bundle{ID: "storage"}, storageRef.Provider, storageResolver)

	stageAction := action.New("test.stage", func(ctx context.Context, input any) (any, error) {
		values, ok := input.(map[string]any)
		if !ok {
			return nil, compiler.SourceError(compiler.Position{}, "stage input is %T, want map[string]any", input)
		}
		ref, ok := values["asset"].(xfile.Ref)
		if !ok {
			return nil, compiler.SourceError(compiler.Position{}, "stage asset is %T, want xfile.Ref", values["asset"])
		}
		resolver := contracts.FileResolverFromContext(ctx)
		if resolver == nil {
			return nil, compiler.SourceError(compiler.Position{}, "file resolver is missing from action context")
		}
		reader, info, err := resolver.Open(ctx, ref)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return map[string]any{
			"content":  string(data),
			"provider": string(info.Provider),
			"digest":   info.Digest,
		}, nil
	}).Build()
	stageBundle := compiler.Bundle{
		ID:        "test-stage",
		Libraries: []action.Library{{Name: "test", Actions: []action.AnyAction{stageAction}}},
	}

	cfg, err := BuildConfig([]compiler.Bundle{embedBundle, storageBundle, stageBundle, runtimeext.Bundle(nil)})
	if err != nil {
		t.Fatal(err)
	}

	storageReader, storageInfo, err := cfg.FileResolver.Open(t.Context(), storageRef)
	if err != nil {
		t.Fatalf("open through composed Storage resolver: %v", err)
	}
	storageData, readErr := io.ReadAll(storageReader)
	closeErr := storageReader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(storageData) != "storage asset" || storageInfo.Provider != storageRef.Provider {
		t.Fatalf("composed Storage result = %q, %#v", storageData, storageInfo)
	}

	ctx := xctx.WithUserID(t.Context(), "test-caller")
	ctx = xctx.WithTenantID(ctx, "test-tenant")
	result, err := Execute(ctx, cfg, "@embed logo \"assets/logo.svg\"\ntest.stage @{ asset: embed.logo }", "test.nflow", nil)
	if err != nil {
		t.Fatalf("execute staged embed: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("stage output = %T, want map[string]any", result.Output)
	}
	if output["content"] != string(asset) || output["provider"] != "flow.static" {
		t.Fatalf("staged output = %#v", output)
	}
	if output["digest"] == "" {
		t.Fatalf("staged file info omitted digest: %#v", output)
	}
}

func TestFileResolverReturnsCloseErrorBeforeNotFoundFallback(t *testing.T) {
	ref := xfile.Ref{Provider: "test.storage", ID: "opaque-id"}
	closeErr := errors.New("close failed")
	secondCalled := false
	resolvers := providerFileResolvers{
		ref.Provider: {
			testFileResolver(func(context.Context, xfile.Ref) (io.ReadCloser, xfile.Info, error) {
				return closeErrorReadCloser{Reader: strings.NewReader(""), err: closeErr}, xfile.Info{}, xerr.NotFound("missing")
			}),
			testFileResolver(func(_ context.Context, requested xfile.Ref) (io.ReadCloser, xfile.Info, error) {
				secondCalled = true
				return io.NopCloser(strings.NewReader("asset")), xfile.Info{Provider: requested.Provider, ID: requested.ID}, nil
			}),
		},
	}
	if _, _, err := newFileResolver(resolvers).Open(t.Context(), ref); !errors.Is(err, closeErr) {
		t.Fatalf("composed resolver error = %v, want close error", err)
	}
	if secondCalled {
		t.Fatal("composed resolver fell through after a reader close error")
	}
}

type testFileResolver func(context.Context, xfile.Ref) (io.ReadCloser, xfile.Info, error)

func (resolver testFileResolver) Open(ctx context.Context, ref xfile.Ref) (io.ReadCloser, xfile.Info, error) {
	return resolver(ctx, ref)
}

type closeErrorReadCloser struct {
	io.Reader
	err error
}

func (reader closeErrorReadCloser) Close() error {
	return reader.err
}
