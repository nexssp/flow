package kernelcoverage

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type manifest struct {
	KernelVersion string       `json:"kernel_version"`
	Scope         string       `json:"scope"`
	Capabilities  []capability `json:"capabilities"`
}

type capability struct {
	ID          string   `json:"id"`
	KernelAPI   string   `json:"kernel_api"`
	Status      string   `json:"status"`
	FlowSymbols []string `json:"flow_symbols"`
	Reason      string   `json:"reason"`
}

// TestKernelActionCoverage checks a deliberately small set of public Kernel
// action APIs against production Flow call sites. It detects drift; it does
// not prove semantic equivalence or require Flow to expose every Kernel API.
func TestKernelActionCoverage(t *testing.T) {
	root := repositoryRoot(t)
	manifestPath := filepath.Join(root, "internal", "kernelcoverage", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read coverage manifest: %v", err)
	}

	var m manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		t.Fatalf("decode coverage manifest: %v", err)
	}
	if len(m.Capabilities) == 0 {
		t.Fatal("coverage manifest contains no capabilities")
	}
	if strings.TrimSpace(m.Scope) == "" {
		t.Fatal("coverage manifest scope must not be empty")
	}

	pinnedVersion := pinnedKernelVersion(t, filepath.Join(root, "go.mod"))
	if m.KernelVersion != pinnedVersion {
		t.Fatalf("manifest audits Kernel %s, but go.mod pins %s; review and update the manifest", m.KernelVersion, pinnedVersion)
	}

	kernelActionDir := kernelActionSourceDir(t, root, m.KernelVersion)
	kernelAPIs := declaredKernelAPIs(t, kernelActionDir)
	flowReferences := productionFlowReferences(t, root)

	seen := make(map[string]struct{}, len(m.Capabilities))
	for _, c := range m.Capabilities {
		t.Run(c.ID, func(t *testing.T) {
			if c.ID == "" {
				t.Fatal("capability id must not be empty")
			}
			if _, exists := seen[c.ID]; exists {
				t.Fatalf("duplicate capability id %q", c.ID)
			}
			seen[c.ID] = struct{}{}
			if c.Reason == "" {
				t.Fatal("capability reason must not be empty")
			}
			if _, ok := kernelAPIs[c.KernelAPI]; !ok {
				t.Fatalf("Kernel API %q no longer exists in %s; review the pinned source and manifest", c.KernelAPI, kernelActionDir)
			}
			if len(c.FlowSymbols) == 0 {
				t.Fatal("at least one Flow symbol is required to check the expected status")
			}

			var hits []string
			for _, symbol := range c.FlowSymbols {
				hits = append(hits, flowReferences[symbol]...)
			}

			switch c.Status {
			case "wired", "partial":
				if len(hits) == 0 {
					t.Errorf("status is %q but none of Flow symbols %v is referenced by production Go code; review the implementation and manifest", c.Status, c.FlowSymbols)
				}
			case "unexposed":
				if len(hits) > 0 {
					t.Errorf("status is unexposed but Flow now references %v at %s; review the capability and update its status", c.FlowSymbols, strings.Join(hits, ", "))
				}
			default:
				t.Fatalf("unknown status %q; use wired, partial, or unexposed", c.Status)
			}
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate coverage test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func pinnedKernelVersion(t *testing.T, goModPath string) string {
	t.Helper()
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	pattern := regexp.MustCompile(`(?m)^\s*github\.com/nexssp/kernel\s+(v\S+)\s*(?://.*)?$`)
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("could not find the pinned github.com/nexssp/kernel version in go.mod")
	}
	return string(match[1])
}

func kernelActionSourceDir(t *testing.T, root, version string) string {
	t.Helper()
	vendorDir := filepath.Join(root, "vendor", "github.com", "nexssp", "kernel", "action")
	if isDir(vendorDir) {
		return vendorDir
	}

	cmd := exec.CommandContext(t.Context(), "go", "env", "GOMODCACHE")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("locate Go module cache: %v", err)
	}
	moduleDir := filepath.Join(strings.TrimSpace(string(output)), "github.com", "nexssp", "kernel@"+version)
	actionDir := filepath.Join(moduleDir, "action")
	if !isDir(actionDir) {
		t.Fatalf("pinned Kernel action source not found at %s; ensure the module is available in the Go module cache", actionDir)
	}
	return actionDir
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func declaredKernelAPIs(t *testing.T, actionDir string) map[string]struct{} {
	t.Helper()
	apis := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(actionDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil {
				apis[fn.Name.Name] = struct{}{}
				continue
			}
			if len(fn.Recv.List) == 0 {
				continue
			}
			if receiver := receiverName(fn.Recv.List[0].Type); receiver != "" {
				apis[receiver+"."+fn.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Kernel action APIs: %v", err)
	}
	return apis
}

func receiverName(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.Ident:
		return typ.Name
	case *ast.StarExpr:
		return receiverName(typ.X)
	case *ast.IndexExpr:
		return receiverName(typ.X)
	case *ast.IndexListExpr:
		return receiverName(typ.X)
	case *ast.SelectorExpr:
		return typ.Sel.Name
	default:
		return ""
	}
}

func productionFlowReferences(t *testing.T, root string) map[string][]string {
	t.Helper()
	references := make(map[string][]string)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			position := fset.Position(selector.Sel.Pos())
			relative, relErr := filepath.Rel(root, position.Filename)
			if relErr != nil {
				relative = position.Filename
			}
			references[selector.Sel.Name] = append(references[selector.Sel.Name], relative+":"+strconv.Itoa(position.Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan production Flow Go files: %v", err)
	}
	return references
}
