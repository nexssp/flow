package fsio

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
	".git",
	"node_modules",
	"vendor",
	"target",
	"dist",
	"build",
	"_srcpack",
	".next",
	".turbo",
	".cache",
	".idea",
	".vscode",
}

// WalkConfig contains strongly-typed parameters for filesystem traversal.
//
// IncludeHidden defaults to false, so hidden files and directories are
// skipped unless the caller explicitly opts in. The zero value is the
// safe choice: no accidental traversal of .git internals, .env files,
// editor swap files, or hidden build artefacts. Set IncludeHidden: true
// to traverse them.
//
// IncludeAll disables the DefaultSkipDirs blacklist. It is independent
// of IncludeHidden: a caller can include vendor/ without including
// .git, and vice versa.
type WalkConfig struct {
	Dir           string   `json:"dir" cli:"dir,d"`
	Dirs          []string `json:"dirs" cli:"dirs"`
	Files         []string `json:"files" cli:"files,file,f"`
	Skip          []string `json:"skip" cli:"skip"`
	IncludeHidden bool     `json:"include_hidden" cli:"include_hidden"`
	IncludeAll    bool     `json:"include_all" cli:"include_all"`
	Ext           string   `json:"ext" cli:"ext"`
	MaxDepth      int      `json:"max_depth" cli:"max_depth"`
}

// WalkReq is an alias to WalkConfig for callers that used the older
// name. Kept deliberately: a rename with no behavioural difference does
// not warrant churning every call site.
type WalkReq = WalkConfig

// WalkSource creates a stream source for file metadata traversal.
//
// Traversal rules, in order:
//
//  1. Hidden entries (name starting with ".") are skipped unless
//     IncludeHidden is true. The rule applies to files and directories
//     alike, so .hidden.go and .hidden/ are treated the same way.
//  2. Directories named in DefaultSkipDirs, or in Skip, are skipped
//     unless IncludeAll is true.
//  3. Entries at depth >= MaxDepth are not descended into; entries
//     deeper than MaxDepth are not yielded.
//  4. Files with an extension in BinaryExtensions are skipped.
//  5. When Ext is set, only files matching one of its extensions are
//     yielded.
//
// The source stops cleanly when the downstream yield returns false,
// when the context is cancelled, or when it has exhausted its target
// directories.
func WalkSource() action.AnyStreamAction {
	return action.NewStream("fs.walk",
		func(ctx context.Context, config WalkConfig) (iter.Seq2[FileMeta, error], error) {
			targetDirs := resolveTargetDirs(config)
			skipMap := buildSkipMap(config)
			extSet := parseExtSet(config.Ext)

			return func(yield func(FileMeta, error) bool) {
				for _, directory := range targetDirs {
					if err := ctx.Err(); err != nil {
						var zero FileMeta
						yield(zero, err)
						return
					}

					absPath, err := filepath.Abs(directory)
					if err != nil {
						var zero FileMeta
						yield(zero, err)
						return
					}

					stopTraversal := false

					_ = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, walkErr error) error {
						if stopTraversal || ctx.Err() != nil {
							return fs.SkipAll
						}

						if walkErr != nil {
							if path == absPath {
								var zero FileMeta
								if !yield(zero, walkErr) {
									stopTraversal = true
									return fs.SkipAll
								}
								return walkErr
							}
							return nil
						}

						relPath, _ := filepath.Rel(absPath, path)
						depth := calculateDepth(relPath)

						name := d.Name()
						isRoot := path == absPath

						// Hidden entries are skipped by default.
						// The check applies to both files and
						// directories. The root itself is never
						// skipped: if the caller explicitly walks
						// ".git", that is a deliberate override.
						if !config.IncludeHidden && !isRoot && strings.HasPrefix(name, ".") {
							if d.IsDir() {
								return fs.SkipDir
							}
							return nil
						}

						if d.IsDir() {
							if isRoot {
								return nil
							}

							if skipMap[strings.ToLower(name)] {
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

						if len(extSet) > 0 {
							trimmed := strings.TrimPrefix(ext, ".")
							if !extSet[trimmed] {
								return nil
							}
						}

						fileInfo, infoErr := d.Info()
						if infoErr != nil {
							return nil
						}

						meta := FileMeta{
							Path:    path,
							RelPath: filepath.ToSlash(relPath),
							Size:    fileInfo.Size(),
							ModTime: fileInfo.ModTime(),
							Lang:    detectLang(path),
						}

						if !yield(meta, nil) {
							stopTraversal = true
							return fs.SkipAll
						}

						return nil
					})

					if stopTraversal {
						return
					}
				}
			}, nil
		},
	)
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
	result := make(map[string]bool)
	if !config.IncludeAll {
		for _, dir := range DefaultSkipDirs {
			result[strings.ToLower(dir)] = true
		}
	}
	for _, dir := range config.Skip {
		trimmed := strings.TrimSpace(dir)
		if trimmed != "" {
			result[strings.ToLower(trimmed)] = true
		}
	}
	return result
}

func calculateDepth(relPath string) int {
	if relPath == "." || relPath == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(relPath), "/") + 1
}
