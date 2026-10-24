package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidateBundle_ExportedSourceModules(t *testing.T) {
	fsys := fstest.MapFS{"lib/critic.nflow": &fstest.MapFile{Data: []byte("@pipeline critic\n@end\n")}}
	if err := ValidateBundle(Bundle{
		ID:              "source-test",
		SourceModules:   fsys,
		ExportedModules: []string{"lib/critic.nflow"},
	}); err != nil {
		t.Fatalf("ValidateBundle(valid source module) error = %v", err)
	}

	for _, tc := range []struct {
		name    string
		bundle  Bundle
		wantErr string
	}{
		{
			name:    "missing filesystem",
			bundle:  Bundle{ID: "source-test", ExportedModules: []string{"lib/critic.nflow"}},
			wantErr: "require SourceModules",
		},
		{
			name:    "traversal",
			bundle:  Bundle{ID: "source-test", SourceModules: fsys, ExportedModules: []string{"../secret.nflow"}},
			wantErr: "invalid exported source module path",
		},
		{
			name:    "non-nflow file",
			bundle:  Bundle{ID: "source-test", SourceModules: fsys, ExportedModules: []string{"lib/readme.md"}},
			wantErr: "invalid exported source module path",
		},
		{
			name:    "missing module",
			bundle:  Bundle{ID: "source-test", SourceModules: fsys, ExportedModules: []string{"lib/missing.nflow"}},
			wantErr: "exported source module",
		},
		{
			name:    "duplicate module",
			bundle:  Bundle{ID: "source-test", SourceModules: fsys, ExportedModules: []string{"lib/critic.nflow", "lib/critic.nflow"}},
			wantErr: "duplicate exported source module",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateBundle(tc.bundle); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBundle() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestBundleSourceLoader_LoadsExportedModulesAndPreservesLocalLoader(t *testing.T) {
	t.Parallel()
	bundle := Bundle{
		ID:    "si-ai",
		Alias: "ai",
		SourceModules: fstest.MapFS{
			"lib/critic.nflow":       &fstest.MapFile{Data: []byte("critic workflow")},
			"lib/common.nflow":       &fstest.MapFile{Data: []byte("shared workflow")},
			"internal/private.nflow": &fstest.MapFile{Data: []byte("private workflow")},
		},
		ExportedModules: []string{"lib/critic.nflow", "lib/common.nflow"},
	}
	loader := newBundleSourceLoader([]Bundle{bundle})
	localDir := t.TempDir()
	localPath := filepath.Join(localDir, "local.nflow")
	if err := os.WriteFile(localPath, []byte("local workflow"), 0o600); err != nil {
		t.Fatal(err)
	}
	withFallback, ok := loader.withFallback(fileSystemLoader{}).(*bundleSourceLoader)
	if !ok {
		t.Fatal("bundle source loader fallback changed loader type")
	}
	loader = withFallback

	data, resolved, err := loader.Load(filepath.Join(localDir, "root.nflow"), "ai:lib/critic.nflow")
	if err != nil {
		t.Fatalf("Load(bundle module) error = %v", err)
	}
	if string(data) != "critic workflow" || resolved != "bundle://si-ai/lib/critic.nflow" {
		t.Fatalf("Load(bundle module) = (%q, %q), want (%q, %q)", data, resolved, "critic workflow", "bundle://si-ai/lib/critic.nflow")
	}

	data, resolved, err = loader.Load("bundle://si-ai/lib/critic.nflow", "./common.nflow")
	if err != nil {
		t.Fatalf("Load(relative bundle module) error = %v", err)
	}
	if string(data) != "shared workflow" || resolved != "bundle://si-ai/lib/common.nflow" {
		t.Fatalf("Load(relative bundle module) = (%q, %q), want (%q, %q)", data, resolved, "shared workflow", "bundle://si-ai/lib/common.nflow")
	}

	data, resolved, err = loader.Load("bundle://si-ai/lib/critic.nflow", "../internal/private.nflow")
	if err != nil || string(data) != "private workflow" || resolved != "bundle://si-ai/internal/private.nflow" {
		t.Fatalf("Load(private bundle-relative dependency) = (%q, %q, %v), want private module from same bundle", data, resolved, err)
	}

	data, resolved, err = loader.Load(filepath.Join(localDir, "root.nflow"), "./local.nflow")
	if err != nil {
		t.Fatalf("Load(local file) error = %v", err)
	}
	if string(data) != "local workflow" || resolved != localPath {
		t.Fatalf("Load(local file) = (%q, %q), want (%q, %q)", data, resolved, "local workflow", localPath)
	}
}

func TestBundleSourceLoader_RejectsPrivateAndUnknownModules(t *testing.T) {
	t.Parallel()
	loader := newBundleSourceLoader([]Bundle{{
		ID:              "si-ai",
		Alias:           "ai",
		SourceModules:   fstest.MapFS{"lib/critic.nflow": &fstest.MapFile{Data: []byte("public")}, "internal/private.nflow": &fstest.MapFile{Data: []byte("private")}},
		ExportedModules: []string{"lib/critic.nflow"},
	}}).withFallback(fileSystemLoader{})

	for _, tc := range []struct {
		target  string
		wantErr string
	}{
		{target: "ai:internal/private.nflow", wantErr: "private or not exported"},
		{target: "ai:../lib/critic.nflow", wantErr: "invalid bundle module path"},
		{target: "missing:lib/critic.nflow", wantErr: "qualifier \"missing\" is not loaded"},
	} {
		_, _, err := loader.Load("root.nflow", tc.target)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("Load(%q) error = %v, want substring %q", tc.target, err, tc.wantErr)
		}
	}
}

func TestFileSystemLoader_ExplainsBundleSourceRequirement(t *testing.T) {
	t.Parallel()
	_, _, err := (fileSystemLoader{}).Load("root.nflow", "ai:lib/critic.nflow")
	if err == nil || !strings.Contains(err.Error(), "requires a loaded @require bundle") {
		t.Fatalf("Load(bundle-qualified path) error = %v, want missing-bundle diagnostic", err)
	}
}
