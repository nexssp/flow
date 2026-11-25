package builder

import (
	"os"
	"path/filepath"
	"strings"
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

func TestBuildArgsPreserveMetadataForDebugAndRelease(t *testing.T) {
	for _, test := range []struct {
		name string
		opts buildOptions
	}{
		{name: "debug", opts: buildOptions{debug: true}},
		{name: "release", opts: buildOptions{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ldflags := linkerFlags(test.opts, "v1.0.0", "abc123", "2026-11-25T00:00:00Z")
			args := buildArgs("nflow", test.opts, ldflags)
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "-ldflags="+ldflags) {
				t.Fatalf("%s build args omitted metadata linker flags: %v", test.name, args)
			}
			wantBuildVCS := !test.opts.debug
			if gotBuildVCS := strings.Contains(joined, "-buildvcs=false"); gotBuildVCS != wantBuildVCS {
				t.Fatalf("%s build args include -buildvcs=false = %t, want %t: %v", test.name, gotBuildVCS, wantBuildVCS, args)
			}
			wantStripped := !test.opts.debug
			if gotStripped := strings.Contains(ldflags, "-s -w"); gotStripped != wantStripped {
				t.Fatalf("%s linker flags contain symbol stripping = %t, want %t: %q", test.name, gotStripped, wantStripped, ldflags)
			}
		})
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
