package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPathRejectsMalformedRequirementLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.nflow")
	if err := os.WriteFile(path, []byte("runtime.const @{ value: \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", []byte("{ malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var code int
	_, stderr := captureIO(t, func() {
		code = RunPath(context.Background(), []string{path})
	})
	if code == 0 {
		t.Fatal("RunPath accepted a malformed present requirement lock")
	}
	for _, want := range []string{"requirement lock", "invalid requirement lock"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
}

func TestLegacyRequireRejectsMalformedRequirementLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.nflow")
	source := "@require example.com/acme v1.0.0\nruntime.const @{ value: \"ok\" }\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", []byte("{ malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "requires_gen.go")

	var code int
	_, stderr := captureIO(t, func() {
		code = RunContext(context.Background(), []string{"require", path, "-o", out})
	})
	if code == 0 {
		t.Fatal("legacy require accepted a malformed present requirement lock")
	}
	for _, want := range []string{"requirement lock", "invalid requirement lock"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("glue output exists despite rejected lock (stat error: %v)", err)
	}
}

func TestRequireFreezeCommandsRequireExactlyOneFile(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "canonical missing", args: []string{"require", "freeze"}, want: "usage: nflow require freeze <file.nflow>"},
		{name: "canonical extra", args: []string{"require", "freeze", "flow.nflow", "extra"}, want: "usage: nflow require freeze <file.nflow>"},
		{name: "require pin missing", args: []string{"require", "pin"}, want: "usage: nflow require pin <file.nflow>"},
		{name: "require pin extra", args: []string{"require", "pin", "flow.nflow", "extra"}, want: "usage: nflow require pin <file.nflow>"},
		{name: "top-level pin missing", args: []string{"pin"}, want: "usage: nflow pin <file.nflow>"},
		{name: "top-level pin extra", args: []string{"pin", "flow.nflow", "extra"}, want: "usage: nflow pin <file.nflow>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var code int
			_, stderr := captureIO(t, func() {
				code = runCLI(context.Background(), nil, tt.args)
			})
			if code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %q, want usage %q", stderr, tt.want)
			}
		})
	}
}

func TestRequireFreezeHelpAndCompletion(t *testing.T) {
	var help strings.Builder
	if !PrintCommandHelp(&help, "require") {
		t.Fatal("require help is not registered")
	}
	for _, want := range []string{"nflow require freeze <file.nflow>", "nflow require pin"} {
		if !strings.Contains(help.String(), want) {
			t.Errorf("require help missing %q", want)
		}
	}
	var pinHelp strings.Builder
	if !PrintCommandHelp(&pinHelp, "pin") || !strings.Contains(pinHelp.String(), "nflow pin <file.nflow>") {
		t.Error("top-level pin compatibility help is missing")
	}

	var script strings.Builder
	if err := Completion("bash", &script); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"require)", "flags=\"freeze pin\"", "pin|run|build"} {
		if !strings.Contains(script.String(), want) {
			t.Errorf("Bash completion missing %q", want)
		}
	}
}
