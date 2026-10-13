// Package files validates static file declarations shared by Flow extensions.
// It does not retain bytes, open providers, or implement runtime storage.
package files

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"
)

var digestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+:[0-9a-fA-F]+$`)

// ValidatePath accepts one canonical slash-separated path below a declared
// fs.FS/root. It rejects absolute paths, traversal, empty components, and
// platform-specific separators so the same reference is safe on every OS.
func ValidatePath(name string) error {
	if name == "" || name == "." || strings.Contains(name, `\`) || path.IsAbs(name) || !fs.ValidPath(name) {
		return xerr.Validation(fmt.Sprintf("invalid file path %q: expected a safe relative path", name))
	}
	if path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." {
		return xerr.Validation(fmt.Sprintf("invalid file path %q: path traversal is not allowed", name))
	}
	return nil
}

// ValidateMap validates the typed Files-map contract. The map is inspected
// only; callers keep ownership and no runtime file state is created.
func ValidateMap(values map[string]xfile.Ref) error {
	for name, ref := range values {
		if err := ValidatePath(name); err != nil {
			return fmt.Errorf("files[%q]: %w", name, err)
		}
		if strings.TrimSpace(string(ref.Provider)) == "" || strings.TrimSpace(string(ref.ID)) == "" || strings.TrimSpace(string(ref.Version)) == "" {
			return xerr.Validation(fmt.Sprintf("files[%q]: provider, ID, and version are required", name))
		}
		if ref.ExpectedSize < 0 {
			return xerr.Validation(fmt.Sprintf("files[%q]: expected size cannot be negative", name))
		}
		if !digestPattern.MatchString(string(ref.ExpectedDigest)) {
			return xerr.Validation(fmt.Sprintf("files[%q]: expected digest must use an explicit algorithm prefix", name))
		}
	}
	return nil
}

// ValidateCompilerMeta checks a compiler metadata Files map while preserving
// the typed xfile.Ref contract; malformed or string-valued maps are rejected.
func ValidateCompilerMeta(meta map[string]any, key string) error {
	if meta == nil {
		return xerr.Validation("files metadata is nil")
	}
	value, ok := meta[key]
	if !ok {
		return nil
	}
	values, ok := value.(map[string]xfile.Ref)
	if !ok {
		return xerr.Validation(fmt.Sprintf("files metadata %q must be map[string]xfile.Ref", key))
	}
	return ValidateMap(values)
}
