package compiler

import (
	"fmt"
	"path/filepath"

	"github.com/nexssp/kernel/xerr"
)

// SourceMap is an immutable-during-use snapshot of source files keyed by their
// cleaned absolute paths. It implements SourceLoader without reading from the
// filesystem. Callers must not mutate the map while any Load call is in progress.
type SourceMap map[string]string

// Load resolves target relative to currentFile's directory (unless target is
// absolute), then looks up the cleaned absolute path in the snapshot. The
// returned byte slice is independent of the stored string, and resolvedPath is
// the canonical lookup identity used for include cycle detection.
func (sources SourceMap) Load(currentFile, target string) (data []byte, resolvedPath string, err error) {
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(currentFile), target)
	}
	resolvedPath, err = filepath.Abs(resolved)
	if err != nil {
		return nil, "", err
	}
	resolvedPath = filepath.Clean(resolvedPath)

	content, ok := sources[resolvedPath]
	if !ok {
		return nil, "", xerr.NotFound(fmt.Sprintf("source %q not found in snapshot", resolvedPath))
	}
	return []byte(content), resolvedPath, nil
}
