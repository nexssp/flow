package lang

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// installEnsure is the production Ensure impl: run the per-OS recipe, then
// re-resolve via the language's Select (find-only — no recursion).
//
// Pure Go: no nexssp/ensure, no Node.js. Recipes call winget/brew/apt/npm
// directly via os/exec. The recipe lives in the runner file's install:
// block; this file is the generic runner.
//
// KISS: one recipe per (language, OS). No version matrices — that's
// flow_version's territory.
type installEnsure struct{}

// installer runs package-manager commands only when installation is explicitly
// requested. Runtime language programs use the separate external.Exec adapter.
type installer interface {
	Run(ctx context.Context, cmd string, args []string) error
}

type defaultInstaller struct{}

func (defaultInstaller) Run(ctx context.Context, cmd string, args []string) error {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

var installerHook installer = defaultInstaller{}

// Ensure implements Ensure. Tries the recipe, re-resolves, returns.
//
// Failure modes (all return NotInstalledError so preflight groups them):
//   - no recipe for this OS  -> NotInstalledError with "no auto-install"
//   - recipe command missing -> NotInstalledError with the underlying error
//   - recipe exits non-zero  -> NotInstalledError with stderr
//   - recipe runs, LookPath still fails -> NotInstalledError (PATH not
//     refreshed in this process — common on Windows after winget)
func (installEnsure) Ensure(lang *Language, version string) (cmd string, args []string, err error) {
	recipe := lang.install.RecipeFor()
	if len(recipe) == 0 {
		return "", nil, &NotInstalledError{
			ID: lang.ID, Version: version,
			Cause: fmt.Errorf("no auto-install recipe for %s on %s; install manually", lang.ID, runtime.GOOS),
		}
	}
	if runErr := installerHook.Run(context.Background(), recipe[0], recipe[1:]); runErr != nil {
		return "", nil, &NotInstalledError{
			ID: lang.ID, Version: version,
			Cause: fmt.Errorf("install recipe %v: %w", recipe, runErr),
		}
	}
	// Re-resolve via Select (find-only). No recursion into Ensure.
	cmd, args, err = lang.Select(version)
	if err == nil {
		return cmd, args, nil
	}
	return "", nil, &NotInstalledError{
		ID: lang.ID, Version: version,
		Cause: errors.New("installed but not on PATH (may need shell restart)"),
	}
}

// Install is the package-level entry point used by `nflow lang install <id>`.
// Wraps installEnsure so callers don't construct the type.
func Install(id, version string) (cmd string, args []string, err error) {
	lang, lookupErr := Lookup(id, version)
	if lookupErr != nil {
		return "", nil, lookupErr
	}
	return installEnsure{}.Ensure(lang, version)
}
