package lang

import (
	"os"
	"testing"

	"github.com/nexssp/flow/compiler"
	runtimeext "github.com/nexssp/flow/extensions/runtime"
	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestCompilerPreflightPy3FixtureUsesStubbedEnsure(t *testing.T) {
	const fixturePath = "testdata/py3_preflight_autoinstall.nflow"
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read py3 preflight fixture %q: %v", fixturePath, err)
	}

	const key = "python@3"
	originalLanguage, ok := Registry[key]
	if !ok {
		t.Fatalf("runner %q is not registered", key)
	}
	forcedMiss := *originalLanguage
	selectCalls := 0
	forcedMiss.Select = func(version string) (string, []string, error) {
		selectCalls++
		if version != "3" {
			t.Errorf("py3 Select version = %q; want 3", version)
		}
		return "", nil, ErrNotFound
	}
	Registry[key] = &forcedMiss
	t.Cleanup(func() { Registry[key] = originalLanguage })

	ensureCalls := 0
	previousEnsure := ensureHook
	ensureHook = installHookFunc(func(language *Language, version string) (string, []string, error) {
		ensureCalls++
		if language.ID != "python" || version != "3" {
			t.Errorf("Ensure called for lang.%s@%s; want lang.python@3", language.ID, version)
		}
		return "/stub/python3", nil, nil
	})
	t.Cleanup(func() { ensureHook = previousEnsure })

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntaxext.Bundle(nil),
		runtimeext.Bundle(nil),
		Bundle(nil),
	})
	if err != nil {
		t.Fatalf("build compiler config: %v", err)
	}
	if _, err := runner.Compile(t.Context(), cfg, string(source), fixturePath); err != nil {
		t.Fatalf("compile py3 .nflow fixture with stubbed preflight: %v", err)
	}
	if selectCalls != 1 {
		t.Errorf("py3 Select called %d times; want 1", selectCalls)
	}
	if ensureCalls != 1 {
		t.Errorf("stubbed Ensure called %d times; want 1", ensureCalls)
	}
}
