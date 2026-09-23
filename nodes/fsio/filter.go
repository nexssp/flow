package fsio

import (
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"
)

type FilterConfig struct {
	Ext       string `json:"ext"`
	Tests     bool   `json:"tests"`
	Generated bool   `json:"generated"`
	Deps      bool   `json:"deps"`
	Examples  bool   `json:"examples"`
	Exclude   string `json:"exclude"`
}

func Filter(cfg FilterConfig) action.StreamOp[FileMeta, FileMeta] {
	extSet := parseExtSet(cfg.Ext)
	var excludePatterns []string
	if cfg.Exclude != "" {
		for _, p := range strings.Split(cfg.Exclude, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				excludePatterns = append(excludePatterns, p)
			}
		}
	}

	return action.StreamFilter(func(f FileMeta) bool {
		if cfg.Tests && isTestFile(f.Path) {
			return false
		}
		if cfg.Generated && isGeneratedFile(f.Path) {
			return false
		}
		if cfg.Deps && isDepPath(f.RelPath) {
			return false
		}
		if cfg.Examples && isExamplePath(f.RelPath) {
			return false
		}
		if len(excludePatterns) > 0 && matchesExclude(f.RelPath, excludePatterns) {
			return false
		}
		if len(extSet) > 0 {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")
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
