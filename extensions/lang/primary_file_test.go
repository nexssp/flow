package lang

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestLangFileKeywordResolvesPathBesideNFlowSource(t *testing.T) {
	root := t.TempDir()
	flowDir := filepath.Join(root, "workflows")
	scriptDir := filepath.Join(flowDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(flowDir, "analysis.nflow")
	scriptPath := filepath.Join(scriptDir, "analyze.py")
	wantSource := "print('from file')\n"
	if err := os.WriteFile(scriptPath, []byte(wantSource), 0o600); err != nil {
		t.Fatal(err)
	}

	primaries := compiler.NewPrimaryExtensionTable(langTagPrimary{
		keyword:  "py.file",
		tag:      "py",
		fileMode: true,
	})
	parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries,
		`py.file "scripts/analyze.py"`, flowPath)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blockExpr, ok := expr.(*langBlockExpr)
	if !ok {
		t.Fatalf("Parse returned %T; want *langBlockExpr", expr)
	}
	absScriptPath, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if blockExpr.block.SourceFile != absScriptPath {
		t.Errorf("SourceFile = %q; want %q", blockExpr.block.SourceFile, absScriptPath)
	}
	if blockExpr.block.Source != wantSource {
		t.Errorf("Source = %q; want %q", blockExpr.block.Source, wantSource)
	}
	if blockExpr.block.Tag != "py.file" {
		t.Errorf("Tag = %q; want py.file", blockExpr.block.Tag)
	}
}

func TestLangFileKeywordRejectsEmptyAndMissingPaths(t *testing.T) {
	primaries := compiler.NewPrimaryExtensionTable(langTagPrimary{
		keyword:  "py.file",
		tag:      "py",
		fileMode: true,
	})
	for _, tc := range []struct {
		name, source, want string
	}{
		{name: "empty path", source: `py.file ""`, want: "script path cannot be empty"},
		{name: "missing file", source: `py.file "missing.py"`, want: "read script"},
		{name: "unquoted path", source: `py.file missing.py`, want: "expected a quoted script path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries, tc.source, filepath.Join(t.TempDir(), "flow.nflow"))
			_, err := parser.Parse()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v; want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLangFileKeywordAutocreatesDeclaredGoTemplate(t *testing.T) {
	flowDir := t.TempDir()
	flowPath := filepath.Join(flowDir, "flow.nflow")
	scriptPath := filepath.Join(flowDir, "normalize.go")
	primaries := compiler.NewPrimaryExtensionTable(langTagPrimary{
		keyword:  "go.file",
		tag:      "go",
		fileMode: true,
	})
	parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries,
		`go.file "normalize.go" :autocreate`, flowPath)
	expr, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	blockExpr, ok := expr.(*langBlockExpr)
	if !ok {
		t.Fatalf("Parse returned %T; want *langBlockExpr", expr)
	}
	language, err := Lookup("go", "")
	if err != nil {
		t.Fatalf("Lookup go: %v", err)
	}
	got, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("ReadFile generated starter: %v", err)
	}
	if string(got) != language.spec.FileTemplate {
		t.Fatalf("generated file = %q; want declared Go template %q", got, language.spec.FileTemplate)
	}
	if blockExpr.block.Source != language.spec.FileTemplate {
		t.Errorf("block source = %q; want declared Go template", blockExpr.block.Source)
	}
	if blockExpr.block.SourceFile != scriptPath {
		t.Errorf("SourceFile = %q; want %q", blockExpr.block.SourceFile, scriptPath)
	}
}

func TestLangFileAutocreateNeverOverwritesAndCanBeDisabled(t *testing.T) {
	flowDir := t.TempDir()
	primaries := compiler.NewPrimaryExtensionTable(langTagPrimary{
		keyword:  "go.file",
		tag:      "go",
		fileMode: true,
	})

	t.Run("existing file is preserved", func(t *testing.T) {
		path := filepath.Join(flowDir, "existing.go")
		const original = "package main\n// user changes\n"
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries,
			`go.file "existing.go" :autocreate`, filepath.Join(flowDir, "flow.nflow"))
		if _, err := parser.Parse(); err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != original {
			t.Fatalf("existing file changed to %q", got)
		}
	})

	t.Run("false disables creation", func(t *testing.T) {
		path := filepath.Join(flowDir, "disabled.go")
		parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries,
			`go.file "disabled.go" :autocreate=false`, filepath.Join(flowDir, "flow.nflow"))
		if _, err := parser.Parse(); err == nil || !strings.Contains(err.Error(), "read script") {
			t.Fatalf("Parse error = %v; want missing-script error", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("disabled autocreate created file; Stat error = %v", err)
		}
	})
}

func TestLangFileAutocreateRequiresRunnerTemplate(t *testing.T) {
	flowDir := t.TempDir()
	path := filepath.Join(flowDir, "missing.sh")
	primaries := compiler.NewPrimaryExtensionTable(langTagPrimary{
		keyword:  "sh.file",
		tag:      "sh",
		fileMode: true,
	})
	parser := compiler.NewParserWithFile(t.Context(), compiler.NewOperatorTable(), primaries,
		`sh.file "missing.sh" :autocreate`, filepath.Join(flowDir, "flow.nflow"))
	if _, err := parser.Parse(); err == nil || !strings.Contains(err.Error(), "does not declare a file_template") {
		t.Fatalf("Parse error = %v; want missing file_template error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file without template was created; Stat error = %v", err)
	}
}

func TestRunLangFileBlockRoundtripPreservesPathWithSpaces(t *testing.T) {
	language, err := Lookup("python", "3")
	if err != nil {
		t.Skipf("python@3 not registered: %v", err)
	}
	if _, _, err := language.Select("3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "scientist script with spaces.py")
	source := `import json, sys
payload = json.load(sys.stdin)
print(json.dumps({"value": payload["value"] + 1, "script": sys.argv[0].split("/")[-1]}))
`
	if err := os.WriteFile(filePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	previous := execRunner
	execRunner = defaultExecutor{}
	t.Cleanup(func() { execRunner = previous })

	result, err := runLangBlock(context.Background(), &LangBlock{
		ID:         "python",
		Version:    "3",
		Tag:        "py.file",
		Source:     source,
		SourceFile: filePath,
	}, map[string]any{"value": 41})
	if err != nil {
		t.Fatalf("runLangBlock: %v", err)
	}
	got, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T; want map[string]any", result)
	}
	if got["value"] != float64(42) {
		t.Errorf("result value = %v; want 42", got["value"])
	}
	if got["script"] != filepath.Base(filePath) {
		t.Errorf("script argument = %v; want %q", got["script"], filepath.Base(filePath))
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("result is not JSON-serializable: %v", err)
	}
}
