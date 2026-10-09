package fs

import (
	"context"
	"io/fs"
	"iter"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"
)

var BinaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".bin": true, ".obj": true, ".o": true, ".a": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".webp": true, ".pdf": true,
	".zip": true, ".tar": true, ".gz": true, ".7z": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
	".pyc": true, ".pyo": true, ".wasm": true,
}

var DefaultSkipDirs = []string{
	".git", "node_modules", "vendor", "target", "dist", "build",
	"_srcpack", ".next", ".turbo", ".cache", ".idea", ".vscode",
}

type WalkConfig struct {
	Dir           string   `json:"dir"            cli:"dir,d"`
	Dirs          []string `json:"dirs"           cli:"dirs"`
	Files         []string `json:"files"          cli:"files,file,f"`
	Skip          []string `json:"skip"           cli:"skip"`
	IncludeHidden bool     `json:"include_hidden" cli:"include_hidden"`
	IncludeAll    bool     `json:"include_all"    cli:"include_all"`
	Ext           string   `json:"ext"            cli:"ext"`
	MaxDepth      int      `json:"max_depth"      cli:"max_depth"`
}

func WalkSource() action.AnyStreamAction {
	return action.NewStream("fs.walk",
		func(ctx context.Context, config WalkConfig) (iter.Seq2[FileMeta, error], error) {
			targetDirs := resolveTargetDirs(config)
			skipSet := buildSkipMap(config)
			extSet := parseExtSet(config.Ext)

			return func(yield func(FileMeta, error) bool) {
				for _, directory := range targetDirs {
					if err := ctx.Err(); err != nil {
						yield(FileMeta{}, err)
						return
					}
					if !walkOneDir(directory, config, skipSet, extSet, yield) {
						return
					}
				}
			}, nil
		},
	)
}

func walkOneDir(
	directory string,
	config WalkConfig,
	skipSet map[string]bool,
	extSet map[string]bool,
	yield func(FileMeta, error) bool,
) bool {
	absPath, err := filepath.Abs(directory)
	if err != nil {
		yield(FileMeta{}, err)
		return false
	}

	stop := false
	walkErr := filepath.WalkDir(absPath, func(path string, d fs.DirEntry, walkErr error) error {
		if stop {
			return fs.SkipAll
		}
		if walkErr != nil {
			// The root itself is the only fatal case: yield the error
			// and stop. Non-root errors are skipped because a single
			// unreadable file must not abort the whole traversal.
			// walkErr is not "swallowed" — it is either propagated via
			// yield or it triggers SkipAll.
			if path == absPath {
				if !yield(FileMeta{}, walkErr) {
					stop = true
					return fs.SkipAll
				}
			}
			return nil
		}
		return visitEntry(path, d, absPath, config, skipSet, extSet, &stop, yield)
	})
	if walkErr != nil && !stop {
		yield(FileMeta{}, walkErr)
		return false
	}
	return !stop
}

func visitEntry(
	path string,
	entry fs.DirEntry,
	root string,
	config WalkConfig,
	skipSet, extSet map[string]bool,
	stop *bool,
	yield func(FileMeta, error) bool,
) error {
	isRoot := path == root
	name := entry.Name()
	relPath, relErr := filepath.Rel(root, path)
	if relErr != nil {
		//nolint:nilerr // path is always under root; unreachable in practice
		return nil
	}
	depth := calculateDepth(relPath)

	if !config.IncludeHidden && !isRoot && strings.HasPrefix(name, ".") {
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	}

	if entry.IsDir() {
		if isRoot {
			return nil
		}
		if skipSet[strings.ToLower(name)] {
			return fs.SkipDir
		}
		if config.MaxDepth > 0 && depth >= config.MaxDepth {
			return fs.SkipDir
		}
		return nil
	}

	if config.MaxDepth > 0 && depth > config.MaxDepth {
		return nil
	}

	ext := strings.ToLower(filepath.Ext(name))
	if BinaryExtensions[ext] {
		return nil
	}
	if len(extSet) > 0 && !extSet[strings.TrimPrefix(ext, ".")] {
		return nil
	}

	info, infoErr := entry.Info()
	if infoErr != nil {
		//nolint:nilerr // unreadable entry: skip it, do not abort the walk
		return nil
	}

	meta := FileMeta{
		Path:    path,
		RelPath: filepath.ToSlash(relPath),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Lang:    detectLang(path),
	}
	if !yield(meta, nil) {
		*stop = true
		return fs.SkipAll
	}
	return nil
}

func resolveTargetDirs(config WalkConfig) []string {
	if len(config.Dirs) > 0 {
		return config.Dirs
	}
	if config.Dir != "" {
		return []string{config.Dir}
	}
	return []string{"."}
}

func buildSkipMap(config WalkConfig) map[string]bool {
	out := make(map[string]bool)
	if !config.IncludeAll {
		for _, dir := range DefaultSkipDirs {
			out[strings.ToLower(dir)] = true
		}
	}
	for _, dir := range config.Skip {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			out[strings.ToLower(trimmed)] = true
		}
	}
	return out
}

func calculateDepth(relPath string) int {
	if relPath == "." || relPath == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(relPath), "/") + 1
}
