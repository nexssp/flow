package fs

import (
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"
)

// FilterConfig controls fs.filter. All fields are optional; the zero
// value passes every file through.
type FilterConfig struct {
	Ext       string `json:"ext"       cli:"ext"`
	Tests     bool   `json:"tests"`
	Generated bool   `json:"generated"`
	Deps      bool   `json:"deps"`
	Examples  bool   `json:"examples"`
	Exclude   string `json:"exclude"`
}

// Filter returns a stream operator that drops files matching any of
// the configured exclusion rules.
func Filter(cfg FilterConfig) action.StreamOp[FileMeta, FileMeta] {
	extSet := parseExtSet(cfg.Ext)
	excludePatterns := splitList(cfg.Exclude)

	return action.StreamFilter(func(meta FileMeta) bool {
		if cfg.Tests && isTestFile(meta.Path) {
			return false
		}
		if cfg.Generated && isGeneratedFile(meta.Path) {
			return false
		}
		if cfg.Deps && isDepPath(meta.RelPath) {
			return false
		}
		if cfg.Examples && isExamplePath(meta.RelPath) {
			return false
		}
		if len(excludePatterns) > 0 && matchesExclude(meta.RelPath, excludePatterns) {
			return false
		}
		if len(extSet) > 0 {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(meta.Path)), ".")
			if !extSet[ext] {
				return false
			}
		}
		return true
	})
}

func FilterOperator() action.NamedOperator {
	return action.NewOperator("fs.filter", Filter)
}

func splitList(csv string) []string {
	if csv == "" {
		return nil
	}
	var out []string
	for p := range strings.SplitSeq(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isTestFile recognizes the test-file naming conventions of Go,
// TypeScript/JavaScript, and Python.
func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	for _, suffix := range []string{
		".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.js", ".spec.js", ".test.jsx", ".spec.jsx",
		"_test.py",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
}

// isGeneratedFile recognizes the common "checked-in but produced" names.
func isGeneratedFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	for _, suffix := range []string{
		".pb.go", "_generated.go", "_generated.ts", ".gen.go",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// isDepPath reports whether any path segment is a dependency cache.
func isDepPath(rel string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		switch strings.ToLower(part) {
		case "node_modules", "vendor", ".pnpm", "third_party":
			return true
		}
	}
	return false
}

// isExamplePath reports whether any path segment is an example/sample dir.
func isExamplePath(rel string) bool {
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		switch strings.ToLower(part) {
		case "examples", "example", "_examples", "samples":
			return true
		}
	}
	return false
}

// matchesExclude reports whether relPath matches any of the shell-style
// patterns in patterns. Patterns may be filenames ("*.go"), full
// relative paths ("internal/*.go"), or directory prefixes ("testdata/").
func matchesExclude(relPath string, patterns []string) bool {
	relSlash := filepath.ToSlash(relPath)
	base := filepath.Base(relPath)

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		patternSlash := filepath.ToSlash(pattern)

		if matched, matchErr := filepath.Match(pattern, base); matchErr == nil && matched {
			return true
		}
		if matched, matchErr := filepath.Match(patternSlash, relSlash); matchErr == nil && matched {
			return true
		}
		trimmed := strings.Trim(patternSlash, "/")
		if strings.HasPrefix(relSlash, trimmed+"/") ||
			strings.Contains(relSlash, "/"+trimmed+"/") {
			return true
		}
	}
	return false
}
