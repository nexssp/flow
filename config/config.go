// Package config provides minimal helpers for loading application
// configuration files. It intentionally does not implement overlay,
// merging, validation, profile chains, env interpolation, or file
// watching — those are domain-specific policies.
package config

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nexssp/kernel/xerr"
)

// Find walks up from startDir looking for filename. It stops at the
// filesystem root. Returns the absolute path of the first regular file
// found, or "" when none exists. An empty startDir uses the current
// working directory.
func Find(filename, startDir string) string {
	if filename == "" {
		return ""
	}
	if startDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		startDir = cwd
	}
	current, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(current, filename)
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// Load reads each path in order and decodes into dst. Fields present
// in later files override fields present in earlier files; fields
// absent from later files are left untouched. Unknown keys are
// rejected. Format is selected by extension: .yaml/.yml (yaml.v3),
// .json (encoding/json). Any other extension returns xerr.BadRequest.
// An empty path is skipped. An empty file is a no-op.
func Load(dst any, paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := loadOne(dst, path); err != nil {
			return err
		}
	}
	return nil
}

// LoadFromDir combines Find and Load. It returns nil when no file named
// filename exists anywhere between startDir and the filesystem root —
// callers use this when "no config file" means "keep defaults". A
// present-but-unreadable or malformed file still returns an error.
func LoadFromDir(dst any, filename, startDir string) error {
	path := Find(filename, startDir)
	if path == "" {
		return nil
	}
	return Load(dst, path)
}

func loadOne(dst any, path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return xerr.NotFound("config file not found: "+path, err)
		}
		return xerr.Internal("open config "+path, err)
	}
	defer func() { _ = file.Close() }()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		decoder := yaml.NewDecoder(file)
		decoder.KnownFields(true)
		if err := decoder.Decode(dst); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return xerr.Validation("parse yaml "+path, err)
		}
	case ".json":
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return xerr.Validation("parse json "+path, err)
		}
	default:
		return xerr.BadRequest("unsupported config extension: " + filepath.Ext(path))
	}
	return nil
}
