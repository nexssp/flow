package fsio

import (
	"path/filepath"
	"strings"
	"time"
)

// FileMeta is the item type that flows through the fsio pipeline.
type FileMeta struct {
	Path    string    `json:"path"`
	RelPath string    `json:"rel_path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Lang    string    `json:"lang"`
}

func detectLang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".py", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	case ".md", ".mdx":
		return "markdown"
	case ".yml", ".yaml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	}
	return ""
}

func parseExtSet(csv string) map[string]bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, e := range strings.Split(csv, ",") {
		e = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
		if e != "" {
			out[e] = true
		}
	}
	return out
}

func isTestFile(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	for _, suf := range []string{
		".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.js", ".spec.js", ".test.jsx", ".spec.jsx",
		"_test.py",
	} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	return false
}

func isGeneratedFile(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	for _, suf := range []string{
		".pb.go", "_generated.go", "_generated.ts", ".gen.go",
	} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

func isDepPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		switch strings.ToLower(part) {
		case "node_modules", "vendor", ".pnpm", "third_party":
			return true
		}
	}
	return false
}

func isExamplePath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		switch strings.ToLower(part) {
		case "examples", "example", "_examples", "samples":
			return true
		}
	}
	return false
}

func matchesExclude(relPath string, patterns []string) bool {
	relSlash := filepath.ToSlash(relPath)
	base := filepath.Base(relPath)

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		patternSlash := filepath.ToSlash(pattern)

		// 1. Dopasowanie po nazwie pliku
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		// 2. Dopasowanie po całej ścieżce względnej
		if matched, _ := filepath.Match(patternSlash, relSlash); matched {
			return true
		}
		// 3. Proste dopasowanie prefiksu / katalogu
		if strings.Contains(relSlash, "/"+strings.Trim(patternSlash, "/")+"/") ||
			strings.HasPrefix(relSlash, strings.Trim(patternSlash, "/")+"/") {
			return true
		}
	}
	return false
}
