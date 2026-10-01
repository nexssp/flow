package fs

import (
	"path/filepath"
	"strings"
	"time"
)

// FileMeta is the item type that flows through the fs pipeline.
type FileMeta struct {
	Path    string    `json:"path"`
	RelPath string    `json:"rel_path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Lang    string    `json:"lang"`
}

// detectLang maps a file extension to a canonical language name used by
// downstream filters and prompts. Unknown extensions return "".
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

// parseExtSet splits a comma-separated extension list into a set. A
// leading dot is stripped so "go" and ".go" produce the same entry.
// Empty input returns nil.
func parseExtSet(csv string) map[string]bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	out := make(map[string]bool)
	for e := range strings.SplitSeq(csv, ",") {
		e = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
		if e != "" {
			out[e] = true
		}
	}
	return out
}
