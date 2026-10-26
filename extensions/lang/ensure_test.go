package lang

import (
	"errors"
	"testing"
)

// TestResolveHit asserts Resolve returns the command when Select succeeds.
// No call to Ensure should happen.
func TestResolveHit(t *testing.T) {
	called := false
	lang := &Language{
		ID: "test-resolve-hit",
		Select: func(string) (string, []string, error) {
			called = true
			return "/usr/bin/fake", nil, nil
		},
	}
	oldHook := ensureHook
	ensureHook = failingEnsure{t: t}
	defer func() { ensureHook = oldHook }()

	cmd, _, err := Resolve(lang, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !called {
		t.Error("Select was not called")
	}
	if cmd != "/usr/bin/fake" {
		t.Errorf("cmd = %q; want /usr/bin/fake", cmd)
	}
}

// TestResolveMissCallsEnsure asserts Resolve falls through to Ensure on miss.
func TestResolveMissCallsEnsure(t *testing.T) {
	lang := &Language{
		ID: "test-resolve-miss",
		Select: func(string) (string, []string, error) {
			return "", nil, ErrNotFound
		},
	}
	ensureCalled := false
	oldHook := ensureHook
	ensureHook = stubEnsure{called: &ensureCalled, cmd: "/usr/bin/installed", args: nil}
	defer func() { ensureHook = oldHook }()

	cmd, _, err := Resolve(lang, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ensureCalled {
		t.Error("Ensure was not called on Select miss")
	}
	if cmd != "/usr/bin/installed" {
		t.Errorf("cmd = %q; want /usr/bin/installed", cmd)
	}
}

// TestResolveMissReturnsError asserts Resolve returns NotInstalledError
// when both Select and Ensure fail.
func TestResolveMissReturnsError(t *testing.T) {
	lang := &Language{
		ID: "test-resolve-error",
		Select: func(string) (string, []string, error) {
			return "", nil, ErrNotFound
		},
	}
	oldHook := ensureHook
	ensureHook = noEnsure{}
	defer func() { ensureHook = oldHook }()

	_, _, err := Resolve(lang, "3")
	if err == nil {
		t.Fatal("Resolve returned nil error for missing interpreter")
	}
	var nie *NotInstalledError
	if !errors.As(err, &nie) {
		t.Fatalf("Resolve error type %T; want *NotInstalledError", err)
	}
	if nie.ID != "test-resolve-error" || nie.Version != "3" {
		t.Errorf("NotInstalledError = %+v; want ID=test-resolve-error Version=3", nie)
	}
}

// TestNotInstalledErrorMessage asserts the error message includes ID and
// version, and the optional Cause.
func TestNotInstalledErrorMessage(t *testing.T) {
	t.Run("no cause", func(t *testing.T) {
		e := &NotInstalledError{ID: "python", Version: "3"}
		got := e.Error()
		want := "lang.python@3: interpreter not found on PATH"
		if got != want {
			t.Errorf("Error() = %q; want %q", got, want)
		}
	})
	t.Run("with cause", func(t *testing.T) {
		e := &NotInstalledError{
			ID: "rust", Version: "",
			Cause: errors.New("recipe failed"),
		}
		got := e.Error()
		if want := "lang.rust: recipe failed"; got != want {
			t.Errorf("Error() = %q; want %q", got, want)
		}
	})
}

func TestIsWindowsStorePythonAlias(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\Users\mapo\AppData\Local\Microsoft\WindowsApps\python3.exe`, true},
		{`C:\Users\mapo\AppData\Local\Microsoft\WindowsApps\python.exe`, true},
		{`C:\Users\mapo\AppData\Local\Microsoft\WindowsApps\go.exe`, false},
		{`C:\Python312\python.exe`, false},
	}
	for _, tc := range cases {
		if got := isWindowsStorePythonAlias(tc.path); got != tc.want {
			t.Errorf("isWindowsStorePythonAlias(%q) = %t; want %t", tc.path, got, tc.want)
		}
	}
}

// --- helpers -------------------------------------------------------------

type failingEnsure struct{ t *testing.T }

func (f failingEnsure) Ensure(*Language, string) (cmd string, args []string, err error) {
	f.t.Fatal("Ensure should not be called when Select succeeds")
	return "", nil, nil
}

type stubEnsure struct {
	called *bool
	cmd    string
	args   []string
}

func (s stubEnsure) Ensure(*Language, string) (cmd string, args []string, err error) {
	*s.called = true
	return s.cmd, s.args, nil
}
