package directives_test

import (
	"strings"
	"testing"

	"github.com/nexssp/flow"
)

// PreprocessString runs the directive preprocessor over a source
// string and returns the resulting Preprocessed. Errors abort the
// test.
func preprocessString(t *testing.T, src string) *flow.Preprocessed {
	t.Helper()

	pre, err := flow.PreprocessBytes([]byte(src), "hook_test.nflow")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	return pre
}

func TestAtHook_SingleName(t *testing.T) {
	pre := preprocessString(t, `
@hook:audit
log.info @{ message: "hello" }
`)

	if len(pre.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(pre.Hooks))
	}
	if pre.Hooks[0].Name != "audit" {
		t.Fatalf("hook name = %q, want %q", pre.Hooks[0].Name, "audit")
	}
}

func TestAtHook_CommaSeparated(t *testing.T) {
	pre := preprocessString(t, `
@hook:audit,telemetry
log.info @{ message: "hello" }
`)

	if len(pre.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(pre.Hooks))
	}
	names := []string{pre.Hooks[0].Name, pre.Hooks[1].Name}
	if names[0] != "audit" || names[1] != "telemetry" {
		t.Fatalf("names = %v", names)
	}
}

func TestAtHook_BracketedList(t *testing.T) {
	pre := preprocessString(t, `
@hook:[audit, telemetry, metrics]
log.info @{ message: "hello" }
`)

	if len(pre.Hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(pre.Hooks))
	}
	names := []string{
		pre.Hooks[0].Name,
		pre.Hooks[1].Name,
		pre.Hooks[2].Name,
	}
	if names[0] != "audit" || names[1] != "telemetry" || names[2] != "metrics" {
		t.Fatalf("names = %v", names)
	}
}

func TestAtHook_MultipleDirectivesAccumulate(t *testing.T) {
	pre := preprocessString(t, `
@hook:audit
@hook:telemetry
log.info @{ message: "hello" }
`)

	if len(pre.Hooks) != 2 {
		t.Fatalf("expected 2 hooks from two directives, got %d", len(pre.Hooks))
	}
}

func TestAtHook_MissingName(t *testing.T) {
	_, err := flow.PreprocessBytes([]byte(`
@hook:
log.info @{ message: "hello" }
`), "hook_test.nflow")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "requires at least one hook name") {
		t.Fatalf("error = %q", err)
	}
}
