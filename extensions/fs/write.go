package fs

import (
	"errors"
	"iter"
	"os"
	"path/filepath"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

// WriteConfig controls fs.write.
type WriteConfig struct {
	Dir       string `json:"dir"       cli:"dir,d"`
	Mode      uint32 `json:"mode"      cli:"mode"`
	Overwrite bool   `json:"overwrite" cli:"overwrite"`
}

// Write persists each FileContent under Dir, preserving RelPath. It is
// the per-item counterpart to out.file, which aggregates the whole
// stream into one file.
//
// RelPath is validated through xfs.Rel and all file operations use
// xfs.Root so symlinks cannot escape Dir.
func Write(cfg WriteConfig) action.StreamOp[FileContent, FileContent] {
	if cfg.Dir == "" {
		cfg.Dir = "."
	}
	if cfg.Mode == 0 {
		cfg.Mode = 0o600
	}
	overwrite := cfg.Overwrite

	return func(up iter.Seq2[FileContent, error]) iter.Seq2[FileContent, error] {
		return func(yield func(FileContent, error) bool) {
			for item, err := range up {
				if err != nil {
					yield(FileContent{}, err)
					return
				}

				rel := item.RelPath
				if rel == "" {
					rel = filepath.Base(item.Path)
				}

				clean, relErr := xfs.Rel(rel)
				if relErr != nil {
					yield(FileContent{}, xerr.Validation("fs.write: "+rel, relErr))
					return
				}

				if writeErr := writeOne(cfg.Dir, clean, item.Content, os.FileMode(cfg.Mode), overwrite); writeErr != nil {
					yield(FileContent{}, writeErr)
					return
				}

				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func WriteOperator() action.NamedOperator {
	return action.NewOperator("fs.write", Write)
}

func writeOne(dir, rel string, content []byte, mode os.FileMode, overwrite bool) (resultErr error) {
	if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
		return xerr.Internal("fs.write: mkdir", mkdirErr)
	}

	root, rootErr := xfs.OpenRoot(dir)
	if rootErr != nil {
		return xerr.Internal("fs.write: root", rootErr)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, xerr.Internal("fs.write: close root", closeErr))
		}
	}()

	if mkdirErr := root.MkdirAll(filepath.Dir(rel), 0o755); mkdirErr != nil {
		return xerr.Internal("fs.write: mkdir", mkdirErr)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	file, openErr := root.OpenFile(rel, flags, mode)
	if openErr != nil {
		return xerr.Internal("fs.write: open "+rel, openErr)
	}

	_, writeErr := file.Write(content)
	closeErr := file.Close()

	if writeErr != nil {
		return xerr.Internal("fs.write: write "+rel, writeErr)
	}
	if closeErr != nil {
		return xerr.Internal("fs.write: close "+rel, closeErr)
	}
	return nil
}
