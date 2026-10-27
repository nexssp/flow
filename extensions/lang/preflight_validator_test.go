package lang

import (
	"errors"
	"testing"
)

func TestPreflightValidatorAutoInstallsMissingInterpreter(t *testing.T) {
	const id = "preflight-validator-autoinstall-test"
	if _, exists := Registry[id]; exists {
		t.Fatalf("test language %q is already registered", id)
	}
	Register(Language{
		ID:     id,
		Ext:    ".fake",
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	})
	t.Cleanup(func() { delete(Registry, id) })

	var installCalls int
	previousEnsure := ensureHook
	ensureHook = installHookFunc(func(language *Language, _ string) (string, []string, error) {
		installCalls++
		return "/test/bin/" + language.ID, nil, nil
	})
	t.Cleanup(func() { ensureHook = previousEnsure })

	ast := &langBlockExpr{block: &LangBlock{ID: id}}
	if err := preflightValidator(t.Context(), ast, nil); err != nil {
		t.Fatalf("preflightValidator: %v", err)
	}
	if installCalls != 1 {
		t.Fatalf("installer called %d times; want 1", installCalls)
	}
}

func TestPreflightValidatorDoesNotInstallWhenContextDisablesIt(t *testing.T) {
	const id = "preflight-validator-no-install-test"
	Register(Language{
		ID:     id,
		Ext:    ".fake",
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	})
	t.Cleanup(func() { delete(Registry, id) })

	var installCalls int
	previousEnsure := ensureHook
	ensureHook = installHookFunc(func(*Language, string) (string, []string, error) {
		installCalls++
		return "/test/bin/installed", nil, nil
	})
	t.Cleanup(func() { ensureHook = previousEnsure })

	ast := &langBlockExpr{block: &LangBlock{ID: id}}
	err := preflightValidator(WithAutoInstallDisabled(t.Context()), ast, nil)
	if err == nil {
		t.Fatal("preflightValidator returned nil for a missing interpreter")
	}
	if installCalls != 0 {
		t.Fatalf("installer called %d times; want 0", installCalls)
	}
	var report Report
	if !errors.As(err, &report) {
		t.Fatalf("preflight error type = %T; want lang.Report", err)
	}
	if reason := report.SelfTestSkipReason(); reason == "" {
		t.Fatal("missing interpreter did not produce a self-test skip reason")
	}
}

func TestResolveDoesNotInstallWhenContextDisablesIt(t *testing.T) {
	var installCalls int
	previousEnsure := ensureHook
	ensureHook = installHookFunc(func(*Language, string) (string, []string, error) {
		installCalls++
		return "/test/bin/installed", nil, nil
	})
	t.Cleanup(func() { ensureHook = previousEnsure })

	language := &Language{
		ID:     "runtime-no-install-test",
		Select: func(string) (string, []string, error) { return "", nil, ErrNotFound },
	}
	_, _, err := resolveContext(WithAutoInstallDisabled(t.Context()), language, "")
	if err == nil {
		t.Fatal("resolveContext returned nil for a missing interpreter")
	}
	if _, ok := errors.AsType[*NotInstalledError](err); !ok {
		t.Fatalf("error type = %T; want *NotInstalledError", err)
	}
	if installCalls != 0 {
		t.Fatalf("installer called %d times; want 0", installCalls)
	}
}

func TestPreflightValidatorIgnoresTreesWithoutLangBlocks(t *testing.T) {
	if err := preflightValidator(t.Context(), nil, nil); err != nil {
		t.Fatalf("preflightValidator(nil): %v", err)
	}
}
