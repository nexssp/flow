package builder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBuildArgsOutputValidation(t *testing.T) {
	for _, args := range [][]string{
		{"-o"},
		{"--output"},
		{"-o", "--debug", "flow.nflow"},
		{"--output", "-d", "flow.nflow"},
		{"-o="},
		{"--output="},
	} {
		t.Run(args[0], func(t *testing.T) {
			if _, code := parseBuildArgs(args); code == 0 {
				t.Fatalf("parseBuildArgs(%q) accepted an empty or missing output path", args)
			}
		})
	}

	for _, args := range [][]string{
		{"flow.nflow", "-o", "out"},
		{"-o", "out", "flow.nflow"},
		{"flow.nflow", "--output=out"},
	} {
		t.Run(args[0]+"/valid", func(t *testing.T) {
			opts, code := parseBuildArgs(args)
			if code != 0 || opts.output != "out" || len(opts.positional) != 1 || opts.positional[0] != "flow.nflow" {
				t.Fatalf("parseBuildArgs(%q) = %#v, %d; want output out and flow.nflow positional", args, opts, code)
			}
		})
	}
}

func TestRunBuildHelpExitsSuccessfully(t *testing.T) {
	if code := RunBuild(t.Context(), []string{"--help"}, nil); code != 0 {
		t.Fatalf("RunBuild --help exit code = %d, want 0", code)
	}
}

func TestResolveBuildOutputPathRejectsInputOverwrite(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "workflow.nflow")
	if err := os.WriteFile(flow, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveBuildOutputPath(flow, flow); err == nil {
		t.Fatal("resolveBuildOutputPath allowed the output to overwrite the workflow")
	}
}

func TestResolveBuildOutputPathRejectsSymlinkToInput(t *testing.T) {
	root := t.TempDir()
	flow := filepath.Join(root, "workflow.nflow")
	if err := os.WriteFile(flow, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output")
	if err := os.Symlink(flow, output); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveBuildOutputPath(flow, output); err == nil {
		t.Fatal("resolveBuildOutputPath allowed a symlink to overwrite the workflow")
	}
}
