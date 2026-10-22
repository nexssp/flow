package builder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/nexssp/flow/extensions/require"
)

func TestRequireLockEffectiveToolchainCaptured(t *testing.T) {
	workDir, flowPath, _ := newRequireLockFixture(t)
	t.Setenv("GOPROXY", localFileProxyURL(filepath.Join(workDir, "proxy"))+",off")
	t.Setenv("GOSUMDB", "off")

	lockPath, err := FreezeRequirements(t.Context(), flowPath, nil)
	if err != nil {
		t.Fatalf("freeze fixture: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock requirementLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("decode generated lock: %v", err)
	}
	if lock.Toolchain == "" {
		t.Fatal("generated lock has an empty toolchain")
	}
	goVersion, err := exec.CommandContext(t.Context(), "go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatalf("go env GOVERSION: %v", err)
	}
	if got, want := lock.Toolchain, strings.TrimSpace(string(goVersion)); got != want {
		t.Fatalf("lock toolchain = %q, want actual go env GOVERSION %q", got, want)
	}
	parsed, err := modfile.Parse("generated lock go.mod", []byte(lock.GoMod), nil)
	if err != nil {
		t.Fatalf("parse locked GoMod: %v", err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path != "nflow-harness" || parsed.Go == nil {
		t.Fatalf("locked GoMod lacks the expected module and go directives: %+v", parsed)
	}
}

func TestRequireLockEffectiveToolchainCheckedBeforeCache(t *testing.T) {
	workDir, flowPath, _ := newRequireLockFixture(t)
	t.Setenv("GOPROXY", localFileProxyURL(filepath.Join(workDir, "proxy"))+",off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv(harnessCacheEnv, filepath.Join(workDir, "cache"))
	if _, err := FreezeRequirements(t.Context(), flowPath, nil); err != nil {
		t.Fatalf("freeze fixture: %v", err)
	}
	reqs, err := SourceRequires(t.Context(), flowPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureHarnessContext(t.Context(), reqs, flowPath); err != nil {
		t.Fatalf("build initial locked harness runner: %v", err)
	}

	lockPath := flowPath + ".lock"
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock requirementLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("decode generated lock: %v", err)
	}
	lock.Toolchain = "go1.99.0"
	data, err = json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureHarnessContext(t.Context(), reqs, flowPath); err == nil || !strings.Contains(err.Error(), "build toolchain mismatch") {
		t.Fatalf("cached harness retry error = %v, want build toolchain mismatch", err)
	}
}

func TestRequireLockSharedModuleUsesHighestResolvedVersion(t *testing.T) {
	modulePath := "example.invalid/shared/bundles"
	requirements := []require.Requirement{
		{Import: modulePath + "/alpha", ModulePath: modulePath, PackagePath: modulePath + "/alpha", ResolvedVersion: "v1.9.0"},
		{Import: modulePath + "/beta", ModulePath: modulePath, PackagePath: modulePath + "/beta", ResolvedVersion: "v1.10.0"},
	}
	for _, ordered := range [][]require.Requirement{requirements, {requirements[1], requirements[0]}} {
		goMod := harnessGoMod("", "", "", ordered)
		parsed, err := modfile.Parse("harness go.mod", []byte(goMod), nil)
		if err != nil {
			t.Fatalf("parse generated harness go.mod: %v\n%s", err, goMod)
		}
		selected := ""
		for _, requirement := range parsed.Require {
			if requirement.Mod.Path == modulePath {
				selected = requirement.Mod.Version
			}
		}
		if selected != "v1.10.0" {
			t.Errorf("shared module selected version = %q, want v1.10.0 (declaration order: %+v)", selected, ordered)
		}
	}
}

func TestRequireLockGoCommandFailurePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := goCommandFailure(ctx, []string{"mod", "tidy"}, errors.New("process killed"), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("go command failure = %v, want errors.Is(context.Canceled)", err)
	}
}
