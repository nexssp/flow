package builder

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/nexssp/flow/extensions/require"
)

func TestHarnessGoModPreservesWorkspaceReplacement(t *testing.T) {
	localTarget := filepath.Join(t.TempDir(), "local replacement")
	if err := os.MkdirAll(localTarget, 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		line       string
		oldPath    string
		oldVersion string
		newPath    string
		newVersion string
	}{
		{
			name:    "remote target without version selector",
			line:    "replace example.invalid/old => example.invalid/new v1.2.3",
			oldPath: "example.invalid/old", newPath: "example.invalid/new", newVersion: "v1.2.3",
		},
		{
			name:       "remote versioned target",
			line:       "replace example.invalid/old v1.0.0 => example.invalid/new v1.2.3",
			oldPath:    "example.invalid/old",
			oldVersion: "v1.0.0",
			newPath:    "example.invalid/new",
			newVersion: "v1.2.3",
		},
		{
			name:       "version-qualified local replacement",
			line:       "replace example.invalid/old v1.0.0 => " + strconv.Quote(filepath.ToSlash(localTarget)),
			oldPath:    "example.invalid/old",
			oldVersion: "v1.0.0",
			newPath:    localTarget,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			work := filepath.Join(t.TempDir(), "go.work")
			contents := "go 1.23\n" + tc.line + "\n"
			if err := os.WriteFile(work, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}

			generated := harnessGoMod("", "", work, nil)
			parsed, err := modfile.Parse("go.mod", []byte(generated), nil)
			if err != nil {
				t.Fatalf("parse generated go.mod: %v\n%s", err, generated)
			}
			if len(parsed.Replace) != 1 {
				t.Fatalf("generated replacements = %+v, want exactly one\n%s", parsed.Replace, generated)
			}
			got := parsed.Replace[0]
			if got.Old.Path != tc.oldPath || got.Old.Version != tc.oldVersion || !sameReplacementTarget(got.New.Path, tc.newPath) || got.New.Version != tc.newVersion {
				t.Fatalf("generated replacement = %s %s => %s %s, want %s %s => %s %s\n%s",
					got.Old.Path, got.Old.Version, got.New.Path, got.New.Version,
					tc.oldPath, tc.oldVersion, tc.newPath, tc.newVersion, generated)
			}
		})
	}
}

func TestHarnessGoModWorkspaceReplacementOverridesDriver(t *testing.T) {
	driverRoot := t.TempDir()
	driverMod := "module example.invalid/driver\n\ngo 1.23\n\nreplace example.invalid/old v1.0.0 => example.invalid/from-driver v1.2.3\n"
	if err := os.WriteFile(filepath.Join(driverRoot, "go.mod"), []byte(driverMod), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "go.work")
	if err := os.WriteFile(work, []byte("go 1.23\nreplace example.invalid/old v1.0.0 => example.invalid/from-workspace v2.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	generated := harnessGoMod(driverRoot, "", work, nil)
	parsed, err := modfile.Parse("go.mod", []byte(generated), nil)
	if err != nil {
		t.Fatalf("parse generated go.mod: %v\n%s", err, generated)
	}
	matched := 0
	for _, replacement := range parsed.Replace {
		if replacement.Old.Path == "example.invalid/old" && replacement.Old.Version == "v1.0.0" {
			matched++
			if replacement.New.Path != "example.invalid/from-workspace" || replacement.New.Version != "v2.0.0" {
				t.Fatalf("matching replacement = %s %s => %s %s, want workspace target\n%s",
					replacement.Old.Path, replacement.Old.Version, replacement.New.Path, replacement.New.Version, generated)
			}
		}
	}
	if matched != 1 {
		t.Fatalf("generated go.mod has %d copies of the workspace replacement selector, want exactly one\n%s", matched, generated)
	}
}

func TestVersionQualifiedWorkspaceReplacementIsNotWildcard(t *testing.T) {
	target := filepath.Join(t.TempDir(), "versioned target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "go.work")
	contents := "go 1.23\nreplace example.invalid/current v1.2.3 => " + strconv.Quote(filepath.ToSlash(target)) + "\n"
	if err := os.WriteFile(work, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if provider := unversionedPackageProvider("example.invalid/current/pkg", "", work, ""); provider != "" {
		t.Fatalf("version-qualified replacement became an unversioned provider %q", provider)
	}

	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "go.mod"), []byte("module example.invalid/current\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	requirements := []require.Requirement{{Import: "example.invalid/current/pkg", ModulePath: "example.invalid/current"}}
	generated := harnessGoMod("", "", work, requirements, sourceRoot)
	parsed, err := modfile.Parse("go.mod", []byte(generated), nil)
	if err != nil {
		t.Fatalf("parse generated go.mod: %v\n%s", err, generated)
	}
	var wildcard, qualified bool
	for _, replacement := range parsed.Replace {
		if replacement.Old.Path != "example.invalid/current" {
			continue
		}
		if replacement.Old.Version == "" && sameReplacementTarget(replacement.New.Path, sourceRoot) {
			wildcard = true
		}
		if replacement.Old.Version == "v1.2.3" && sameReplacementTarget(replacement.New.Path, target) {
			qualified = true
		}
	}
	if !wildcard || !qualified {
		t.Fatalf("generated replacements wildcard=%t qualified=%t; want both\n%s", wildcard, qualified, generated)
	}
}

func sameReplacementTarget(got, want string) bool {
	if filepath.IsAbs(want) {
		return filepath.Clean(filepath.FromSlash(got)) == filepath.Clean(want)
	}
	return got == want
}
