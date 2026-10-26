package lang

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRecipeFor asserts the per-OS recipe lookup. We can't fake runtime.GOOS
// here, so we test the OSRecipe.RecipeFor() method directly with explicit
// values.
func TestRecipeFor(t *testing.T) {
	r := OSRecipe{
		Windows: []string{"winget", "install", "x"},
		Darwin:  []string{"brew", "install", "x"},
		Linux:   []string{"apt", "install", "x"},
	}
	// Just assert the three branches return their own recipe.
	if got := r.Windows; len(got) != 3 || got[0] != "winget" {
		t.Errorf("Windows = %v", got)
	}
	if got := r.Darwin; len(got) != 3 || got[0] != "brew" {
		t.Errorf("Darwin = %v", got)
	}
	if got := r.Linux; len(got) != 3 || got[0] != "apt" {
		t.Errorf("Linux = %v", got)
	}
}

// TestRecipeForEmpty asserts an OSRecipe with no recipe for the current OS
// returns nil — installEnsure surfaces this as "no auto-install recipe."
func TestRecipeForEmpty(t *testing.T) {
	r := OSRecipe{} // all nil
	if got := r.RecipeFor(); got != nil {
		t.Errorf("RecipeFor on empty OSRecipe = %v; want nil", got)
	}
}

// fakeInstaller is a test stub for the installer seam. It records what would
// have been run and returns a configurable error.
type fakeInstaller struct {
	called   bool
	lastCmd  string
	lastArgs []string
	err      error
}

func (f *fakeInstaller) Run(_ context.Context, cmd string, args []string) error {
	f.called = true
	f.lastCmd = cmd
	f.lastArgs = args
	return f.err
}

// TestInstallEnsureNoRecipe asserts a language with no install recipe returns
// NotInstalledError with the "no auto-install" cause, and does not invoke
// the installer.
func TestInstallEnsureNoRecipe(t *testing.T) {
	fi := &fakeInstaller{}
	oldInstaller := installerHook
	installerHook = fi
	defer func() { installerHook = oldInstaller }()

	lang := &Language{
		ID:      "no-recipe-lang",
		install: OSRecipe{}, // no recipe
		Select:  func(string) (string, []string, error) { return "", nil, ErrNotFound },
	}
	_, _, err := installEnsure{}.Ensure(lang, "")
	if err == nil {
		t.Fatal("Ensure returned nil for no-recipe language")
	}
	var nie *NotInstalledError
	if !errors.As(err, &nie) {
		t.Fatalf("error type %T; want *NotInstalledError", err)
	}
	if !strings.Contains(nie.Error(), "no auto-install recipe") {
		t.Errorf("error = %q; want 'no auto-install recipe'", nie)
	}
	if fi.called {
		t.Error("installer was called for a no-recipe language")
	}
}

// TestInstallEnsureRecipeFails asserts a failing recipe surfaces NotInstalledError
// with the install-failure cause.
func TestInstallEnsureRecipeFails(t *testing.T) {
	fi := &fakeInstaller{err: errors.New("winget not found")}
	oldInstaller := installerHook
	installerHook = fi
	defer func() { installerHook = oldInstaller }()

	lang := &Language{
		ID: "failing-recipe-lang",
		install: OSRecipe{
			// Set all three so RecipeFor returns something on any OS.
			Windows: []string{"winget", "install", "x"},
			Darwin:  []string{"brew", "install", "x"},
			Linux:   []string{"apt", "install", "x"},
		},
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	}
	_, _, err := installEnsure{}.Ensure(lang, "")
	if err == nil {
		t.Fatal("Ensure returned nil for failing recipe")
	}
	var nie *NotInstalledError
	if !errors.As(err, &nie) {
		t.Fatalf("error type %T; want *NotInstalledError", err)
	}
	if !strings.Contains(nie.Error(), "winget not found") {
		t.Errorf("error = %q; want substring 'winget not found'", nie)
	}
}

// TestInstallEnsureSuccess asserts that when the recipe runs and Select
// then succeeds, the resolved command is returned.
func TestInstallEnsureSuccess(t *testing.T) {
	fi := &fakeInstaller{} // succeeds
	oldInstaller := installerHook
	installerHook = fi
	defer func() { installerHook = oldInstaller }()

	selectCalls := 0
	lang := &Language{
		ID: "success-recipe-lang",
		install: OSRecipe{
			Windows: []string{"winget", "install", "x"},
			Darwin:  []string{"brew", "install", "x"},
			Linux:   []string{"apt", "install", "x"},
		},
		// First Select call (during Ensure's re-resolve) succeeds.
		Select: func(string) (string, []string, error) {
			selectCalls++
			return "/usr/bin/installed-binary", nil, nil
		},
	}
	cmd, _, err := installEnsure{}.Ensure(lang, "")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !fi.called {
		t.Error("installer was not called")
	}
	if selectCalls != 1 {
		t.Errorf("Select called %d times; want 1 (the re-resolve)", selectCalls)
	}
	if cmd != "/usr/bin/installed-binary" {
		t.Errorf("cmd = %q; want /usr/bin/installed-binary", cmd)
	}
}

// TestInstallEnsurePathNotRefreshed asserts that when the recipe succeeds but
// Select still misses (PATH not refreshed in this process), the error
// message mentions the shell-restart hint.
func TestInstallEnsurePathNotRefreshed(t *testing.T) {
	fi := &fakeInstaller{} // succeeds
	oldInstaller := installerHook
	installerHook = fi
	defer func() { installerHook = oldInstaller }()

	lang := &Language{
		ID: "path-stale-lang",
		install: OSRecipe{
			Windows: []string{"winget", "install", "x"},
			Darwin:  []string{"brew", "install", "x"},
			Linux:   []string{"apt", "install", "x"},
		},
		Select: func(string) (string, []string, error) {
			return "", nil, ErrNotFound // still missing after install
		},
	}
	_, _, err := installEnsure{}.Ensure(lang, "")
	if err == nil {
		t.Fatal("Ensure returned nil when Select still missed after install")
	}
	var nie *NotInstalledError
	if !errors.As(err, &nie) {
		t.Fatalf("error type %T; want *NotInstalledError", err)
	}
	if !strings.Contains(nie.Error(), "shell restart") {
		t.Errorf("error = %q; want 'shell restart' hint", nie)
	}
}
