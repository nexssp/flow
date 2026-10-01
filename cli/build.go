package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runBuild(args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("o", "", "output binary")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	positional := fs.Args()
	if len(positional) < 1 {
		return fatalf("usage: nflow build <file.nflow> [-o binary]")
	}

	nflowPath, err := filepath.Abs(positional[0])
	if err != nil {
		return fatalf("resolve path: %v", err)
	}

	reqs, err := SourceRequires(nflowPath)
	if err != nil {
		return fatalf("%v", err)
	}

	binName := *out
	if binName == "" {
		base := strings.TrimSuffix(filepath.Base(nflowPath), filepath.Ext(nflowPath))
		binName = base + exeSuffix()
	}

	driverRoot, driverVersion := resolveDriverInfo()
	flowDir := filepath.Dir(nflowPath)
	goworkPath := findGoWork(flowDir)

	buildDir, err := os.MkdirTemp("", "nflow-build-*")
	if err != nil {
		return fatalf("temp: %v", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	src, err := os.ReadFile(nflowPath)
	if err != nil {
		return fatalf("read: %v", err)
	}
	//nolint:gosec // trusted internal temp path
	err = os.WriteFile(filepath.Join(buildDir, "workflow.nflow"), src, 0o600)
	if err != nil {
		return fatalf("embed: %v", err)
	}

	err = os.WriteFile(filepath.Join(buildDir, "main.go"), []byte(buildMain()), 0o600)
	if err != nil {
		return fatalf("main.go: %v", err)
	}

	mod := harnessGoMod(driverRoot, driverVersion, goworkPath, reqs)
	err = os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(mod), 0o600)
	if err != nil {
		return fatalf("go.mod: %v", err)
	}

	fmt.Printf("compiling %s -> %s\n", filepath.Base(nflowPath), binName)

	if err := runGo(buildDir, "mod", "tidy"); err != nil {
		return fatalf("go mod tidy: %v", err)
	}

	if err := runGo(buildDir, "build", "-trimpath", "-o", binName, "."); err != nil {
		return fatalf("build: %v", err)
	}
	fmt.Printf("ok: %s\n", binName)
	return 0
}

func buildMain() string {
	return `package main

import (
	"context"
	_ "embed"
	"os"

	"github.com/nexssp/flow/cli"
)

//go:embed workflow.nflow
var embedded string

func main() {
	os.Exit(cli.RunEmbedded(context.Background(), embedded, os.Args[1:]))
}
`
}
