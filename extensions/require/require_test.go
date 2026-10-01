package require

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseLocalRequirement verifies that relative file paths walk up to find
// the nearest go.mod, establishing the correct canonical import path.
func TestParseLocalRequirement(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	goModContent := []byte("module example.com/localapp\n\ngo 1.26\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), goModContent, 0o600); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(root, "internal", "tool")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := Parse("./internal/tool", root, map[string]string{"timeout": "5s"}, "main.nflow", 1)
	if err != nil {
		t.Fatalf("Parse local requirement failed: %v", err)
	}

	if r.Import != "example.com/localapp/internal/tool" {
		t.Fatalf("import path resolution failed: got %q", r.Import)
	}
	if !r.IsLocal() {
		t.Fatal("expected requirement to be marked as local")
	}
	if r.Options["timeout"] != "5s" {
		t.Fatalf("options not preserved: got %v", r.Options)
	}
}

// TestParseRemoteRequirement covers the three forms a remote requirement
// can take: same-module subpackage without a version, a versioned
// module, and a version that does not start with 'v'.
//
// A missing version is allowed by design: same-module subpackages and
// modules wired through a replace directive resolve without one. A
// version that does not start with 'v' is rejected — go modules use
// semver tags and the DSL should not accept otherwise.
func TestParseRemoteRequirement(t *testing.T) {
	t.Parallel()

	r, err := Parse("github.com/nexssp/cost", "", nil, "test.nflow", 1)
	if err != nil {
		t.Fatalf("remote import without version must be allowed: %v", err)
	}
	if r.Import != "github.com/nexssp/cost" || r.Version != "" {
		t.Fatalf("unexpected requirement fields: %+v", r)
	}
	if r.IsLocal() {
		t.Fatal("remote requirement was incorrectly flagged as local")
	}

	if _, err := Parse("github.com/nexssp/cost 1.0.0", "", nil, "test.nflow", 1); err == nil {
		t.Fatal("expected error on version missing leading 'v'")
	}

	versioned, err := Parse("github.com/nexssp/cost v1.0.0", "", map[string]string{"retries": "3"}, "test.nflow", 1)
	if err != nil {
		t.Fatalf("valid remote requirement rejected: %v", err)
	}
	if versioned.Import != "github.com/nexssp/cost" || versioned.Version != "v1.0.0" {
		t.Fatalf("unexpected requirement fields: %+v", versioned)
	}
	if versioned.Options["retries"] != "3" {
		t.Fatalf("options not preserved: %v", versioned.Options)
	}
}

// TestResolveModulePaths verifies that local modules populate physical
// directory and go.mod locations, while remote modules remain unlinked
// pending build-time resolution.
func TestResolveModulePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module testmod\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	localReq, err := Parse("./", root, nil, "flow.nflow", 1)
	if err != nil {
		t.Fatalf("parse local: %v", err)
	}
	remoteReq, err := Parse("example.com/pkg v0.1.0", "", nil, "flow.nflow", 2)
	if err != nil {
		t.Fatalf("parse remote: %v", err)
	}

	plan := Plan{Modules: []Requirement{localReq, remoteReq}}
	resolved, err := Resolve(plan, root)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved modules, got %d", len(resolved))
	}

	if resolved[0].Dir != root || resolved[0].GoMod != filepath.Join(root, "go.mod") {
		t.Fatalf("local module disk metadata mismatch: %+v", resolved[0])
	}

	if resolved[1].Dir != "" || resolved[1].GoMod != "" {
		t.Fatalf("remote module should not contain local disk paths: %+v", resolved[1])
	}
}
