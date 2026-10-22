package builder

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

const (
	requireLockTestModule  = "example.invalid/requirelock/repo"
	requireLockTestVersion = "v1.4.2"
	requireLockPseudo      = "v0.0.0-20261004235959-abcdef123456"
)

func TestRequireLockFreezeReplayRunBuild(t *testing.T) {
	workDir, flowPath, original := newRequireLockFixture(t)
	proxyDir := filepath.Join(workDir, "proxy")
	proxyURL := localFileProxyURL(proxyDir)
	t.Setenv("GOPROXY", proxyURL+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))

	lockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("freeze requirements: %v", err)
	}
	if want := flowPath + ".lock"; lockPath != want {
		t.Fatalf("lock path = %q, want %q", lockPath, want)
	}
	gotSource, err := os.ReadFile(flowPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotSource, original) {
		t.Fatalf("freezing requirements edited the flow source:\n got %q\nwant %q", gotSource, original)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock requirementLock
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatalf("decode generated lock: %v", err)
	}
	if lock.Format != requirementLockFormat || lock.Go == "" || len(lock.Input) != 64 {
		t.Fatalf("unexpected lock header: format=%d go=%q input=%q", lock.Format, lock.Go, lock.Input)
	}
	if len(lock.Requirements) != 1 {
		t.Fatalf("lock requirements = %d, want 1: %+v", len(lock.Requirements), lock.Requirements)
	}
	gotReq := lock.Requirements[0]
	if gotReq.Import != requireLockTestModule || gotReq.Version != "" || gotReq.Alias != "named" || gotReq.Options["prefix"] != "frozen" {
		t.Fatalf("lock did not preserve source selector/alias/options: %+v", gotReq)
	}
	if gotReq.ResolvedVersion != requireLockTestVersion || gotReq.ModulePath != requireLockTestModule || gotReq.PackagePath != requireLockTestModule+"/nexssflow" {
		t.Fatalf("unexpected frozen provider: %+v", gotReq)
	}
	if len(lock.Modules) == 0 {
		t.Fatal("lock contains no selected module graph")
	}
	providerFound := false
	for i, mod := range lock.Modules {
		if i > 0 && lock.Modules[i-1].Path >= mod.Path {
			t.Fatalf("module graph is not strictly path-sorted: %q then %q", lock.Modules[i-1].Path, mod.Path)
		}
		if mod.Path == requireLockTestModule {
			providerFound = true
		}
		if mod.Replace == nil || mod.Replace.Version != "" {
			if mod.Sum == "" || mod.GoModSum == "" {
				t.Errorf("module %s@%s is missing module/content sums: %+v", mod.Path, mod.Version, mod)
			}
		}
	}
	if !providerFound {
		t.Fatalf("selected module graph omits requested provider %q", requireLockTestModule)
	}
	if !containsLockedModule(lock.Modules, requireLockTestTransitive) {
		t.Fatalf("selected module graph omits transitive module %q", requireLockTestTransitive)
	}
	// Re-freezing identical declarations and provider metadata must serialize a
	// byte-identical graph and checksum snapshot.
	secondLockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("repeat freeze: %v", err)
	}
	secondBytes, err := os.ReadFile(secondLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondBytes, lockBytes) {
		t.Fatal("repeated freeze did not produce deterministic lock bytes")
	}

	// Replay must use the stored package/version metadata and avoid looking up
	// the moving selector. Disable all proxy access after the first resolution.
	reqs, err := SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", "off")
	resolved, replay, err := resolveRequirementsForFlow(t.Context(), reqs, flowPath, mustSelfModuleRoot(t), "", "")
	if err != nil {
		t.Fatalf("replay lock with proxy disabled: %v", err)
	}
	if replay == nil || !reflect.DeepEqual(resolved, lock.Requirements) {
		t.Fatalf("replayed requirements = %+v, lock requirements = %+v", resolved, lock.Requirements)
	}

	// Existing run and standalone-build paths must accept and execute the lock.
	if code := runFlow([]string{flowPath}); code != 0 {
		t.Fatalf("locked flow run returned %d", code)
	}
	binaryPath := filepath.Join(workDir, "locked-flow"+exeSuffix())
	if code := runBuild([]string{flowPath, "-o", binaryPath}); code != 0 {
		t.Fatalf("locked flow build returned %d", code)
	}
	cmd := exec.CommandContext(t.Context(), binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built locked flow failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"value":"frozen:ok"`) {
		t.Fatalf("built flow output did not preserve alias/options: %s", output)
	}
}

func TestRequireLockRejectsMalformedStaleAndTamperedLocks(t *testing.T) {
	workDir, flowPath, _ := newRequireLockFixture(t)
	t.Setenv("GOPROXY", localFileProxyURL(filepath.Join(workDir, "proxy"))+",off")
	t.Setenv("GOSUMDB", "off")
	lockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("freeze fixture: %v", err)
	}
	reqs, err := SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatal(err)
	}
	validBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var valid requirementLock
	if err := json.Unmarshal(validBytes, &valid); err != nil {
		t.Fatal(err)
	}
	checkRejected := func(name string, mutate func(*requirementLock)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Modules = append([]lockedModule(nil), valid.Modules...)
			candidate.Requirements = append([]require.Requirement(nil), valid.Requirements...)
			mutate(&candidate)
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lockPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequirementLock(t.Context(), flowPath, reqs); err == nil {
				t.Fatal("invalid or stale lock was accepted")
			}
		})
	}
	checkRejected("unsupported schema", func(lock *requirementLock) { lock.Format++ })
	checkRejected("stale declarations", func(lock *requirementLock) { lock.Input = strings.Repeat("0", 64) })
	checkRejected("toolchain mismatch", func(lock *requirementLock) { lock.Go = "go0.0.0" })
	checkRejected("malformed checksum", func(lock *requirementLock) {
		lock.Modules[0].Sum = "not-a-checksum"
	})

	// A syntactically valid but changed checksum is rejected when the harness
	// stages and verifies the locked module graph (not just by JSON validation).
	candidate := valid
	candidate.Modules = append([]lockedModule(nil), valid.Modules...)
	provider := -1
	for i := range candidate.Modules {
		if candidate.Modules[i].Path == requireLockTestModule {
			provider = i
			break
		}
	}
	if provider < 0 {
		t.Fatal("fixture lock has no provider module")
	}
	candidate.Modules[provider].Sum = alteredTestSum(candidate.Modules[provider].Sum)
	candidate.GoSum = replaceTestGoSum(candidate.GoSum, requireLockTestModule, candidate.Modules[provider].Version, candidate.Modules[provider].Sum)
	if err := validateRequirementLock(&candidate); err != nil {
		t.Fatalf("altered checksum should remain structurally valid for graph verification: %v", err)
	}
	driverRoot := mustSelfModuleRoot(t)
	dir := t.TempDir()
	if err := writeHarness(dir, driverRoot, "", "", nil, filterExternalRequires(valid.Requirements), filepath.Dir(flowPath)); err != nil {
		t.Fatalf("write verification harness: %v", err)
	}
	if err := prepareRequirementDependencies(t.Context(), dir, driverRoot, &candidate); err == nil {
		t.Fatal("changed module checksum was accepted during dependency preparation")
	}
}

func TestRequireLockMovingBranchRefReplaysPseudoVersionOffline(t *testing.T) {
	workDir, _, _ := newRequireLockFixture(t)
	flowPath := filepath.Join(workDir, "branch.nflow")
	source := fmt.Sprintf(`@require %s main as named { prefix: "branch" }
@assert: result.value == "branch:ok"
{} -> named.echo
`, requireLockTestModule)
	if err := os.WriteFile(flowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", localFileProxyURL(filepath.Join(workDir, "proxy"))+",off")
	t.Setenv("GOSUMDB", "off")
	lockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("freeze moving branch selector: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock requirementLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	if len(lock.Requirements) != 1 || lock.Requirements[0].Version != "main" || lock.Requirements[0].ResolvedVersion != requireLockPseudo {
		t.Fatalf("branch selector was not bound to pseudo-version metadata: %+v", lock.Requirements)
	}
	reqs, err := SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", "off")
	resolved, replay, err := resolveRequirementsForFlow(t.Context(), reqs, flowPath, mustSelfModuleRoot(t), "", "")
	if err != nil {
		t.Fatalf("replay moving ref without remote access: %v", err)
	}
	if replay == nil || !reflect.DeepEqual(resolved, lock.Requirements) {
		t.Fatalf("offline replay = %+v, want frozen resolution %+v", resolved, lock.Requirements)
	}
}

func TestRequireLockRejectsLocalFingerprintDrift(t *testing.T) {
	local := t.TempDir()
	file := filepath.Join(local, "source.go")
	if err := os.WriteFile(file, []byte("package fixture\nconst Value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock := &requirementLock{Modules: []lockedModule{{
		Path:    "example.invalid/local/dependency",
		Version: "v1.0.0",
		Replace: &lockedModule{Path: local, Fingerprint: moduleSourceFingerprint(local)},
	}}}
	if err := validateLockedLocalModules(lock); err != nil {
		t.Fatalf("fresh local replacement fingerprint rejected: %v", err)
	}
	if err := os.WriteFile(file, []byte("package fixture\nconst Value = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateLockedLocalModules(lock); err == nil {
		t.Fatal("changed local replacement content was accepted")
	}
}

func TestRequireLockCancellationDoesNotReplaceExistingLock(t *testing.T) {
	_, flowPath, _ := newRequireLockFixture(t)
	lockPath := flowPath + ".lock"
	old := []byte("previous-lock-must-survive\n")
	if err := os.WriteFile(lockPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := FreezeRequirements(ctx, flowPath, nil); err == nil {
		t.Fatal("freeze with canceled context succeeded")
	}
	got, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("old lock disappeared: %v", err)
	}
	if !reflect.DeepEqual(got, old) {
		t.Fatalf("canceled freeze overwrote old lock: %q", got)
	}
}

const requireLockTestTransitive = "example.invalid/requirelock/transitive"

func newRequireLockFixture(t *testing.T) (workDir, flowPath string, sourceBytes []byte) {
	t.Helper()
	workDir = t.TempDir()
	proxyDir := filepath.Join(workDir, "proxy")
	writeGoModuleProxy(t, proxyDir, requireLockTestModule, requireLockTestVersion, map[string]string{
		"nexssflow/library.go": testBundleSource("requirelock"),
	})
	writeGoModuleProxy(t, proxyDir, requireLockTestModule, requireLockPseudo, map[string]string{
		"nexssflow/library.go": testBundleSource("requirelock"),
	})
	writeGoModuleProxy(t, proxyDir, requireLockTestTransitive, "v1.0.0", map[string]string{
		"helper.go": "package transitive\nconst Value = 1\n",
	})
	providerMod := filepath.Join(proxyDir, filepath.FromSlash(requireLockTestModule), "@v", requireLockTestVersion+".mod")
	if err := os.WriteFile(providerMod, []byte("module "+requireLockTestModule+"\n\ngo 1.23\n\nrequire "+requireLockTestTransitive+" v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pseudoMod := filepath.Join(proxyDir, filepath.FromSlash(requireLockTestModule), "@v", requireLockPseudo+".mod")
	if err := os.WriteFile(pseudoMod, []byte("module "+requireLockTestModule+"\n\ngo 1.23\n\nrequire "+requireLockTestTransitive+" v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	branchInfo := filepath.Join(proxyDir, filepath.FromSlash(requireLockTestModule), "@v", "main.info")
	if err := os.WriteFile(branchInfo, []byte(`{"Version":"`+requireLockPseudo+`","Time":"2026-10-04T23:59:59Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	listPath := filepath.Join(proxyDir, filepath.FromSlash(requireLockTestModule), "@v", "list")
	if err := os.WriteFile(listPath, []byte(requireLockTestVersion+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	flowPath = filepath.Join(workDir, "frozen.nflow")
	source := fmt.Sprintf(`@require %s as named { prefix: "frozen" }
@assert: result.value == "frozen:ok"
{} -> named.echo
`, requireLockTestModule)
	if err := os.WriteFile(flowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return workDir, flowPath, []byte(source)
}

func containsLockedModule(modules []lockedModule, path string) bool {
	for _, mod := range modules {
		if mod.Path == path {
			return true
		}
	}
	return false
}

func alteredTestSum(checksum string) string {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(checksum, "h1:"))
	if err != nil || len(raw) != sha256.Size {
		sum := sha256.Sum256([]byte("altered checksum"))
		return "h1:" + base64.StdEncoding.EncodeToString(sum[:])
	}
	raw[0] ^= 0xff
	return "h1:" + base64.StdEncoding.EncodeToString(raw)
}

func replaceTestGoSum(data, modulePath, version, checksum string) string {
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == modulePath && fields[1] == version {
			lines[i] = strings.Join([]string{modulePath, version, checksum}, " ")
		}
	}
	return strings.Join(lines, "\n")
}
