package embed

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	fileext "github.com/nexssp/flow/extensions/files"
)

const (
	DefaultMaxTotalBytes = int64(64 << 20)
	DefaultMaxFiles      = 4096
)

// AssetDirectory maps slash-separated paths relative to the embedded directory
// to their file bytes.
type AssetDirectory = map[string][]byte

func readDirectory(fsys fs.FS, root string, maxFileBytes, maxTotalBytes int64, maxFiles int) (AssetDirectory, error) {
	if maxTotalBytes <= 0 || maxFiles <= 0 {
		return nil, errors.New("directory limits must be positive")
	}
	if err := rejectSymlinkComponents(fsys, root); err != nil {
		return nil, err
	}

	files := make(AssetDirectory)
	var totalBytes int64
	err := fs.WalkDir(fsys, root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %q: %w", name, walkErr)
		}
		if name == root {
			if !entry.IsDir() {
				return fmt.Errorf("%q is not a directory", root)
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink path %q is not allowed", name)
		}
		if entry.IsDir() {
			return nil
		}
		if len(files) >= maxFiles {
			return fmt.Errorf("directory exceeds max_files limit %d", maxFiles)
		}

		relative := strings.TrimPrefix(name, root+"/")
		if err := fileext.ValidatePath(relative); err != nil {
			return fmt.Errorf("invalid path %q: %w", relative, err)
		}
		data, err := readAsset(fsys, name, maxFileBytes)
		if err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
		if int64(len(data)) > maxTotalBytes-totalBytes {
			return fmt.Errorf("directory exceeds max_total_size limit %d bytes", maxTotalBytes)
		}
		totalBytes += int64(len(data))
		files[relative] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("directory contains no regular files")
	}
	return files, nil
}
