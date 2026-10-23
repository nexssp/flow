// Package selfbench measures file-discovered Flow workloads in process.
package selfbench

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfs"
)

//go:embed scenarios
var builtins embed.FS

// Scenario is one source file; Name is its portable, suite-relative filter key.
type Scenario struct {
	Name   string `json:"name"`
	Source string `json:"-"`
	File   string `json:"-"`
}

// Discover recursively reads .nflow files in lexical order. An empty path uses
// the embedded suite. Explicit paths select a new filesystem root; symlinks
// within it are rejected, rather than following files outside that suite.
func Discover(path string, filters []string) ([]Scenario, error) {
	var source fs.FS
	var root *xfs.Root
	base := ""
	selected := "."
	if path == "" {
		var err error
		source, err = fs.Sub(builtins, "scenarios")
		if err != nil {
			return nil, xerr.BadRequest("self bench: scenarios: " + err.Error())
		}
	} else {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, xerr.BadRequest("self bench: path: " + err.Error())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, xerr.BadRequest("self bench: symlink path is not allowed")
		}
		base, err = filepath.Abs(path)
		if err != nil {
			return nil, xerr.BadRequest("self bench: path: " + err.Error())
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() || filepath.Ext(path) != ".nflow" {
				return nil, xerr.BadRequest("self bench: expected a .nflow file or directory")
			}
			selected = filepath.Base(base)
			base = filepath.Dir(base)
		}
		root, err = xfs.OpenRoot(base)
		if err != nil {
			return nil, xerr.BadRequest("self bench: root: " + err.Error())
		}
		defer root.Close()
		source = root.FS()
	}
	scenarios := make([]Scenario, 0)
	err := fs.WalkDir(source, selected, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return xerr.BadRequest("self bench: symlink is not allowed: " + path)
		}
		if entry.IsDir() || filepath.Ext(path) != ".nflow" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return xerr.BadRequest("self bench: non-regular scenario: " + path)
		}
		if !matches(path, filters) {
			return nil
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		file := "<selfbench>/" + path
		if base != "" {
			file = filepath.Join(base, filepath.FromSlash(path))
		}
		scenarios = append(scenarios, Scenario{Name: path, File: file, Source: string(data)})
		return nil
	})
	if err != nil {
		return nil, xerr.BadRequest("self bench: discovery: " + err.Error())
	}
	if len(scenarios) == 0 {
		return nil, xerr.BadRequest("self bench: no scenarios matched")
	}
	return scenarios, nil
}

func matches(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if strings.Contains(strings.ToLower(name), strings.ToLower(filter)) {
			return true
		}
	}
	return false
}
