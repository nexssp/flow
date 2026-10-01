package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	isBundle := fs.Bool("bundle", false, "scaffold a nexssflow extension bundle instead of a full project")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	target := "."
	if len(fs.Args()) > 0 && strings.TrimSpace(fs.Args()[0]) != "" {
		target = strings.TrimSpace(fs.Args()[0])
	}

	if *isBundle || target == "nexssflow" || strings.HasPrefix(target, "nexssflow") {
		return scaffoldBundle(target)
	}

	return scaffoldProject(target)
}

func scaffoldProject(targetDir string) int {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fatalf("init: create directory %s: %v", targetDir, err)
	}

	workflowPath := filepath.Join(targetDir, "workflow.nflow")
	if _, err := os.Stat(workflowPath); err == nil {
		fmt.Fprintf(os.Stderr, "⚠️  workflow.nflow already exists in %s\n", targetDir)
		return 0
	}

	helpersDir := filepath.Join(targetDir, "helpers")
	if err := os.MkdirAll(helpersDir, 0o755); err != nil {
		return fatalf("init: create directory %s: %v", helpersDir, err)
	}

	helperCode := `package helpers

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = "helpers"

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	prefix := opts["prefix"]
	if prefix == "" {
		prefix = "FLOW"
	}

	formatAction := action.New("helpers.format", func(_ context.Context, in map[string]any) (map[string]any, error) {
		text, _ := in["text"].(string)
		return map[string]any{
			"formatted": fmt.Sprintf("[%s] %s", prefix, strings.ToUpper(text)),
			"length":    len(text),
		}, nil
	}).Description("Format input text with prefix").Build()

	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{
			{
				Name:    ID,
				Actions: []action.AnyAction{formatAction},
			},
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(helpersDir, "library.go"), []byte(helperCode), 0o600); err != nil {
		return fatalf("init: write library.go: %v", err)
	}

	workflowContent := `@description "Modern Starter Workflow: Pipelines, Parallelism & Assertions"

# Attach local extension bundle
@require ./helpers {
    prefix: "DEMO"
}

@assert: result.length == 18
@assert: result.status == "verified"

# 1. Initialize state
{ text: "hello nexss flow" }

# 2. Execute local extension action
-> helpers.format

# 3. Parallel verification and state projection
-> {
    formatted: .formatted,
    length:    .length,
    status:    "verified"
}
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o600); err != nil {
		return fatalf("init: write %s: %v", workflowPath, err)
	}

	projectName := filepath.Base(targetDir)
	if projectName == "." {
		if cwd, err := os.Getwd(); err == nil {
			projectName = filepath.Base(cwd)
		}
	}
	readmeContent := fmt.Sprintf(`# %s

Starter Nexss Flow project created with `+"`nflow init`"+`.

## Run

`+"```powershell"+`
nflow run workflow.nflow -v
`+"```"+`
`, projectName)
	if err := os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readmeContent), 0o600); err != nil {
		return fatalf("init: write README.md: %v", err)
	}

	fmt.Printf("✓ nflow: created project in %s\n\n", targetDir)
	fmt.Printf("  Quick start:\n")
	if targetDir != "." {
		fmt.Printf("    cd %s\n", targetDir)
	}
	fmt.Printf("    nflow run workflow.nflow -v\n\n")
	return 0
}

func scaffoldBundle(targetDir string) int {
	if targetDir == "." {
		targetDir = "nexssflow"
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fatalf("init --bundle: create directory %s: %v", targetDir, err)
	}

	targetFile := filepath.Join(targetDir, "library.go")
	if _, err := os.Stat(targetFile); err == nil {
		fmt.Fprintf(os.Stderr, "⚠️  %s already exists\n", targetFile)
		return 0
	}

	pkgName := filepath.Base(targetDir)
	idName := pkgName
	if idName == "nexssflow" {
		if cwd, err := os.Getwd(); err == nil {
			idName = filepath.Base(cwd)
		}
	}

	content := fmt.Sprintf(`package %s

import (
	"context"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
)

const ID = %q

func init() {
	core.Register(ID, Bundle)
}

func Bundle(opts map[string]string) core.Bundle {
	runAction := action.New("%s.run", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		return map[string]any{
			"status":   "ok",
			"received": in,
		}, nil
	}).Description("Extension action for " + ID).Build()

	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{
			{
				Name:    ID,
				Actions: []action.AnyAction{runAction},
			},
		},
	}
}
`, pkgName, idName, idName)

	if err := os.WriteFile(targetFile, []byte(content), 0o600); err != nil {
		return fatalf("init --bundle: write %s: %v", targetFile, err)
	}

	fmt.Printf("✓ nflow: created extension bundle in %s\n", targetFile)
	return 0
}
