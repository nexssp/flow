package builder

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireLockRuntimeUsesExecutableRole(t *testing.T) {
	lock := &requirementLock{Go: "go1.26.9", Toolchain: "go1.26.10"}
	tests := []struct {
		name    string
		current string
		harness bool
		wantErr bool
	}{
		{name: "parent accepts its recorded Go runtime", current: lock.Go},
		{name: "parent rejects harness toolchain", current: lock.Toolchain, wantErr: true},
		{name: "harness accepts its recorded toolchain", current: lock.Toolchain, harness: true},
		{name: "harness rejects parent Go runtime", current: lock.Go, harness: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequirementRuntime(lock, tc.current, tc.harness)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRequirementRuntime(lock, %q, %t) error = %v, wantErr %t", tc.current, tc.harness, err, tc.wantErr)
			}
		})
	}
}

func TestRequireLockLooseBundleContentDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping loose-bundle lock integration test in short mode")
	}

	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	t.Setenv(harnessCacheEnv, cache)

	bundleDir := filepath.Join(root, "loose")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "go.mod")); !os.IsNotExist(err) {
		t.Fatalf("fixture must be a loose bundle without go.mod; stat error = %v", err)
	}
	bundleID := "loose"
	bundlePath := filepath.Join(bundleDir, "library.go")
	originalBundle := testBundleSource(bundleID)
	if err := os.WriteFile(bundlePath, []byte(originalBundle), 0o600); err != nil {
		t.Fatal(err)
	}

	flowPath := filepath.Join(root, "loose-lock.nflow")
	flowSource := "@require ./loose as named\n@assert: result.value != \"\"\n{} -> named.echo\n"
	if err := os.WriteFile(flowPath, []byte(flowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	reqs, err := SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatalf("read direct loose-bundle declaration: %v", err)
	}
	if len(reqs) != 1 || !reqs[0].IsLoose || reqs[0].LocalPath != bundleDir {
		t.Fatalf("fixture requirement = %+v; want one direct loose-bundle requirement at %s", reqs, bundleDir)
	}

	lockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("freeze loose-bundle requirement: %v", err)
	}
	var lock requirementLock
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatalf("decode frozen lock: %v", err)
	}
	if len(lock.LooseBundles) != 1 || lock.LooseBundles[0].Path != bundleDir || lock.LooseBundles[0].Fingerprint != moduleSourceFingerprint(bundleDir) {
		t.Fatalf("frozen loose-bundle snapshot = %+v; want current fingerprint for %s", lock.LooseBundles, bundleDir)
	}
	if err := ValidateRequirementLock(t.Context(), flowPath, reqs); err != nil {
		t.Fatalf("fresh loose-bundle lock rejected: %v", err)
	}

	// Populate and reuse the runner cache while the frozen content is unchanged.
	bin, err := EnsureHarnessContext(t.Context(), reqs, flowPath)
	if err != nil {
		t.Fatalf("build harness from unchanged lock: %v", err)
	}
	cached, err := EnsureHarnessContext(t.Context(), reqs, flowPath)
	if err != nil {
		t.Fatalf("reuse harness cache from unchanged lock: %v", err)
	}
	if cached != bin {
		t.Fatalf("unchanged lock returned runner %q after initially returning %q", cached, bin)
	}
	if code := ExecHarnessContext(t.Context(), bin, []string{"run", flowPath}); code != 0 {
		t.Fatalf("run unchanged locked loose bundle returned %d", code)
	}
	initialBinary := filepath.Join(root, "initial-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", initialBinary}); code != 0 {
		t.Fatalf("build unchanged locked loose bundle returned %d", code)
	}
	if _, err := os.Stat(initialBinary); err != nil {
		t.Fatalf("initial standalone build output missing: %v", err)
	}

	// Change only the action's returned string; imports, declarations, and flow
	// input remain identical, so the loose-source fingerprint must catch drift.
	changedBundle := strings.Replace(originalBundle, `":ok"`, `":changed"`, 1)
	if changedBundle == originalBundle {
		t.Fatal("fixture action body did not contain the expected literal to mutate")
	}
	if err := os.WriteFile(bundlePath, []byte(changedBundle), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequirementLock(t.Context(), flowPath, reqs); err == nil || !strings.Contains(err.Error(), "loose local bundle content changed") {
		t.Fatalf("changed loose bundle validation error = %v; want stale-content rejection", err)
	}
	// A runner is already cached above. The stale lock must be rejected before
	// that cache can be returned, rather than silently running old compiled code.
	if _, err := EnsureHarnessContext(t.Context(), reqs, flowPath); err == nil || !strings.Contains(err.Error(), "loose local bundle content changed") {
		t.Fatalf("EnsureHarnessContext with stale bundle returned error %v; want rejection before cache reuse", err)
	}
	staleBinary := filepath.Join(root, "stale-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", staleBinary}); code == 0 {
		t.Fatal("standalone build accepted a stale loose-bundle lock")
	}

	// Refreezing is the explicit acknowledgement of the source change.
	if _, err := FreezeRequirements(t.Context(), flowPath, nil); err != nil {
		t.Fatalf("explicitly refreeze changed loose bundle: %v", err)
	}
	if err := ValidateRequirementLock(t.Context(), flowPath, reqs); err != nil {
		t.Fatalf("refrozen loose-bundle lock rejected: %v", err)
	}
	refrozenBin, err := EnsureHarnessContext(t.Context(), reqs, flowPath)
	if err != nil {
		t.Fatalf("harness rejected explicitly refrozen loose bundle: %v", err)
	}
	if code := ExecHarnessContext(t.Context(), refrozenBin, []string{"run", flowPath}); code != 0 {
		t.Fatalf("run explicitly refrozen loose bundle returned %d", code)
	}
	finalBinary := filepath.Join(root, "refrozen-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", finalBinary}); code != 0 {
		t.Fatalf("build explicitly refrozen loose bundle returned %d", code)
	}
	cmd := exec.CommandContext(t.Context(), finalBinary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("execute explicitly refrozen standalone build: %v\n%s", err, output)
	}
}
