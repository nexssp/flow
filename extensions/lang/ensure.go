package lang

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Ensure is the auto-install seam.
//
// Resolve calls Ensure when a language's Select (find-only) fails. The
// default installEnsure runs the runner's per-OS recipe and re-resolves the
// interpreter; tests or hosts can replace the hook when needed.
//
// Pure Go: no nexssp/ensure, no Node.js. Recipes call winget/brew/apt/npm
// directly. See install.go.
type Ensure interface {
	Ensure(lang *Language, version string) (cmd string, args []string, err error)
}

// ensureHook is the package-level seam. Production automatically installs
// through runner recipes; tests can replace it with a deterministic stub.
var ensureHook Ensure = installEnsure{}

type noEnsure struct{}

func (noEnsure) Ensure(lang *Language, version string) (cmd string, args []string, err error) {
	return "", nil, &NotInstalledError{ID: lang.ID, Version: version}
}

// NotInstalledError is the unified "interpreter missing" error. Preflight
// groups these into a single Report; the CLI prints the report and aborts.
type NotInstalledError struct {
	ID, Version string
	Cause       error // optional: why an auto-install attempt failed
}

func (e *NotInstalledError) Error() string {
	prefix := "lang." + e.ID
	if e.Version != "" {
		prefix += "@" + e.Version
	}
	if e.Cause != nil {
		return prefix + ": " + e.Cause.Error()
	}
	return prefix + ": interpreter not found on PATH"
}

func (e *NotInstalledError) Unwrap() error { return e.Cause }

// ErrNotFound is the sentinel from lookPathCmd. Tests assert against this
// rather than a string match.
var ErrNotFound = errors.New("interpreter not found")

// lookPathCmd finds name on PATH, with Windows .cmd/.exe/.bat fallbacks.
//
// exec.LookPath on Windows tries PATHEXT extensions, but its behavior
// across Go versions is inconsistent for shims installed by npm/pnpm
// (bun.cmd, deno.cmd, tsx.cmd). The explicit suffix tries are insurance.
func lookPathCmd(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil && !isWindowsStorePythonAlias(p) {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		for _, ext := range []string{".cmd", ".exe", ".bat"} {
			if p, err := exec.LookPath(name + ext); err == nil && !isWindowsStorePythonAlias(p) {
				return p, nil
			}
		}
	}
	return "", ErrNotFound
}

func isWindowsStorePythonAlias(path string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	if !strings.Contains(normalized, "/microsoft/windowsapps/") {
		return false
	}
	name := strings.TrimSuffix(filepath.Base(normalized), ".exe")
	return name == "python" || name == "python3"
}

// Resolve is the single entry point for "give me a runnable command."
//
// Tries the language's Select (find-only). On miss, calls ensureHook, which
// installs and re-resolves by default or returns a NotInstalledError if the
// runner has no recipe or installation fails. Context-scoped internal callers
// may disable installation without changing this production default.
//
// Preflight, runLangBlock, and the CLI all go through here. There is no
// other call site for lang.Select — Resolve is the gate.
func Resolve(lang *Language, version string) (cmd string, args []string, err error) {
	return resolveContext(context.Background(), lang, version)
}

func resolveContext(ctx context.Context, lang *Language, version string) (cmd string, args []string, err error) {
	if cmd, args, err := lang.Select(version); err == nil {
		return cmd, args, nil
	}
	if autoInstallDisabled(ctx) {
		return "", nil, &NotInstalledError{ID: lang.ID, Version: version, Cause: ErrNotFound}
	}
	return ensureHook.Ensure(lang, version)
}
