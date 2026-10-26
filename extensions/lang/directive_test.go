package lang

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseTagValid exercises the happy path for a representative sample of
// tags. After the data-driven refactor, ParseTag no longer calls Select —
// it only resolves the alias and looks up the Language. Preflight owns the
// interpreter check.
func TestParseTagValid(t *testing.T) {
	// Ensure runners are loaded (other tests may have cleared the registry).
	for k := range Registry {
		delete(Registry, k)
	}
	aliases = nil
	if err := LoadRunners(); err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}

	b, err := ParseTag("py", "print(1)", nil)
	if err != nil {
		t.Fatalf("ParseTag(py) error: %v", err)
	}
	if b.ID != "python" {
		t.Errorf("ParseTag(py).ID = %q; want python", b.ID)
	}
	if b.Version != "" {
		t.Errorf("ParseTag(py).Version = %q; want empty", b.Version)
	}
	if b.Source != "print(1)" {
		t.Errorf("ParseTag(py).Source = %q; want 'print(1)'", b.Source)
	}
}

// TestParseTagVersion asserts the version is captured from the alias.
func TestParseTagVersion(t *testing.T) {
	for k := range Registry {
		delete(Registry, k)
	}
	aliases = nil
	if err := LoadRunners(); err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}

	b, err := ParseTag("py3", "import sys", nil)
	if err != nil {
		t.Fatalf("ParseTag(py3) error: %v", err)
	}
	if b.Version != "3" {
		t.Errorf("ParseTag(py3).Version = %q; want 3", b.Version)
	}
}

// TestParseTagUnknown asserts unknown tags return UnknownTagError.
func TestParseTagUnknown(t *testing.T) {
	_, err := ParseTag("cobol", "IDENTIFICATION DIVISION.", nil)
	if err == nil {
		t.Fatal("ParseTag(cobol) returned nil error for unknown tag")
	}
	var e *UnknownTagError
	if !errors.As(err, &e) {
		t.Fatalf("ParseTag(cobol) error type %T; want *UnknownTagError", err)
	}
	if e.Tag != "cobol" {
		t.Errorf("UnknownTagError.Tag = %q; want cobol", e.Tag)
	}
}

// TestParseTagDoesNotCheckInterpreter asserts ParseTag succeeds even when
// the interpreter is missing. Preflight owns that check. This is the
// contract: a flow with rs where rust isn't installed still parses;
// preflight reports the missing interpreter.
func TestParseTagDoesNotCheckInterpreter(t *testing.T) {
	for k := range Registry {
		delete(Registry, k)
	}
	aliases = nil
	if err := LoadRunners(); err != nil {
		t.Fatalf("LoadRunners: %v", err)
	}

	// "rust" is registered but may not be installed on this machine.
	// ParseTag must succeed regardless.
	b, err := ParseTag("rs", "fn main() {}", nil)
	if err != nil {
		t.Fatalf("ParseTag(rs) error: %v; want nil (preflight owns the check)", err)
	}
	if b.ID != "rust" {
		t.Errorf("ID = %q; want rust", b.ID)
	}
}

// TestRunLangBlockRoundtrip exercises the full runtime path against the
// real python3 interpreter. Skipped if python3 is not on PATH.
func TestRunLangBlockRoundtrip(t *testing.T) {
	lang, err := Lookup("python", "3")
	if err != nil {
		t.Skipf("python@3 not registered: %v", err)
	}
	if _, _, err := lang.Select("3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}

	// Reset executor to default (in case another test swapped it).
	oldExec := execRunner
	execRunner = defaultExecutor{}
	defer func() { execRunner = oldExec }()

	src := `import sys, json
data = json.load(sys.stdin)
print(json.dumps({"value": data["n"] * 2}))
`
	b := &LangBlock{
		ID:      "python",
		Version: "3",
		Source:  src,
	}
	out, err := runLangBlock(context.Background(), b, map[string]int{"n": 21})
	if err != nil {
		t.Fatalf("runLangBlock error: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("runLangBlock returned %T; want map[string]any", out)
	}
	if v, ok := m["value"].(float64); !ok || v != 42 {
		t.Errorf("runLangBlock output = %v; want {value: 42}", out)
	}
}

// TestRunLangBlockSubprocessFailure asserts ExecError is returned when the
// subprocess exits non-zero, and that stderr is captured for diagnostics.
func TestRunLangBlockSubprocessFailure(t *testing.T) {
	lang, err := Lookup("python", "3")
	if err != nil {
		t.Skipf("python@3 not registered: %v", err)
	}
	if _, _, err := lang.Select("3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}

	oldExec := execRunner
	execRunner = defaultExecutor{}
	defer func() { execRunner = oldExec }()

	b := &LangBlock{
		ID:      "python",
		Version: "3",
		Source:  "import sys; sys.stderr.write('boom\\n'); raise SystemExit(1)",
	}
	_, err = runLangBlock(context.Background(), b, nil)
	if err == nil {
		t.Fatal("runLangBlock returned nil error for failing subprocess")
	}
	var ee *ExecError
	if !errors.As(err, &ee) {
		t.Fatalf("runLangBlock error type %T; want *ExecError", err)
	}
	if !strings.Contains(ee.Stderr, "boom") {
		t.Errorf("ExecError.Stderr = %q; want it to contain 'boom'", ee.Stderr)
	}
}

// TestRegisterDefineValid exercises @lang.define's data model: valid input
// produces a registered language runnable through the standard path.
func TestRegisterDefineValid(t *testing.T) {
	const id = "testlang-define"
	defer delete(Registry, id)

	err := RegisterDefine(Define{
		ID:           id,
		Ext:          ".txt",
		Find:         []string{"cat"},
		Stdin:        "json",
		Stdout:       "json",
		FileTemplate: "starter source",
	})
	if err != nil {
		t.Fatalf("RegisterDefine: %v", err)
	}
	l, err := Lookup(id, "")
	if err != nil {
		t.Fatalf("Lookup after RegisterDefine: %v", err)
	}
	if l.spec.FileTemplate != "starter source" {
		t.Errorf("FileTemplate = %q; want starter source", l.spec.FileTemplate)
	}
	cmd, args, err := l.Select("")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	// lookPathCmd returns an absolute path; assert on basename.
	if got := filepath.Base(cmd); got != "cat" {
		t.Errorf("Select basename = %q; want cat", got)
	}
	if len(args) != 0 {
		t.Errorf("Select args = %v; want empty", args)
	}
}

// TestRegisterDefineValidation exercises each required-field check.
func TestRegisterDefineValidation(t *testing.T) {
	cases := []struct {
		name string
		d    Define
		want string
	}{
		{"missing id", Define{Ext: ".x", Find: []string{"x"}}, "id is required"},
		{"missing ext", Define{ID: "x", Find: []string{"x"}}, "ext is required"},
		{"missing find", Define{ID: "x", Ext: ".x"}, "find is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := RegisterDefine(c.d)
			if err == nil {
				t.Fatal("RegisterDefine returned nil for invalid input")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("RegisterDefine error = %q; want substring %q", err, c.want)
			}
		})
	}
}

// TestRegisterDefineDuplicate ensures a re-definition fails gracefully.
func TestRegisterDefineDuplicate(t *testing.T) {
	const id = "testlang-dup"
	defer delete(Registry, id)

	d := Define{ID: id, Ext: ".txt", Find: []string{"cat"}}
	if err := RegisterDefine(d); err != nil {
		t.Fatalf("first RegisterDefine: %v", err)
	}
	err := RegisterDefine(d)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("second RegisterDefine error = %v; want 'already registered'", err)
	}
}

func TestRegisterDefineAliasCollisionReturnsErrorWithoutMutation(t *testing.T) {
	const id = "testlang-alias-collision"
	if _, exists := Registry[id]; exists {
		t.Fatalf("test language %q unexpectedly exists", id)
	}
	err := RegisterDefine(Define{
		ID:      id,
		Ext:     ".x",
		Aliases: []string{"py"},
		Find:    []string{"python3"},
	})
	if err == nil || !strings.Contains(err.Error(), "alias @py already registered") {
		t.Fatalf("RegisterDefine error = %v; want duplicate alias error", err)
	}
	if _, exists := Registry[id]; exists {
		t.Fatalf("failed registration left language %q in Registry", id)
	}
}

func TestRegisterDefineAllowsDistinctVersion(t *testing.T) {
	const version = "lang-test-version"
	key := registryKey("python", version)
	defer delete(Registry, key)
	if _, exists := Registry[key]; exists {
		t.Fatalf("test language version %q unexpectedly exists", key)
	}
	if err := RegisterDefine(Define{
		ID:      "python",
		Version: version,
		Ext:     ".py",
		Find:    []string{"python3"},
	}); err != nil {
		t.Fatalf("RegisterDefine for a distinct version: %v", err)
	}
	if _, err := Lookup("python", version); err != nil {
		t.Fatalf("Lookup for distinct version: %v", err)
	}
}
