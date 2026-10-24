package compiler

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

const bundleSourceURI = "bundle://"

type bundleSource struct {
	id      string
	fsys    fs.FS
	exports map[string]struct{}
}

type bundleSourceLoader struct {
	byName   map[string][]*bundleSource
	bundles  []*bundleSource
	fallback SourceLoader
}

func validateBundleSourceModules(bundle Bundle) error {
	if len(bundle.ExportedModules) > 0 && bundle.SourceModules == nil {
		return fmt.Errorf("bundle %q: exported source modules require SourceModules", bundle.ID)
	}
	seen := make(map[string]struct{}, len(bundle.ExportedModules))
	for _, name := range bundle.ExportedModules {
		if strings.Contains(name, `\`) || !fs.ValidPath(name) || !strings.HasSuffix(name, ".nflow") {
			return fmt.Errorf("bundle %q: invalid exported source module path %q; use a relative .nflow path", bundle.ID, name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("bundle %q: duplicate exported source module %q", bundle.ID, name)
		}
		seen[name] = struct{}{}
		info, err := fs.Stat(bundle.SourceModules, name)
		if err != nil {
			return fmt.Errorf("bundle %q: exported source module %q: %w", bundle.ID, name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("bundle %q: exported source module %q is a directory", bundle.ID, name)
		}
	}
	return nil
}

func newBundleSourceLoader(bundles []Bundle) *bundleSourceLoader {
	loader := &bundleSourceLoader{byName: make(map[string][]*bundleSource)}
	for i := range bundles {
		bundle := &bundles[i]
		if bundle.SourceModules == nil {
			continue
		}
		source := &bundleSource{
			id:      bundle.ID,
			fsys:    bundle.SourceModules,
			exports: make(map[string]struct{}, len(bundle.ExportedModules)),
		}
		for _, name := range bundle.ExportedModules {
			source.exports[name] = struct{}{}
		}
		loader.bundles = append(loader.bundles, source)
		qualifiers := []string{bundle.Alias}
		if validSourceNamespace(bundle.ID) && bundle.ID != bundle.Alias {
			qualifiers = append(qualifiers, bundle.ID)
		}
		for _, qualifier := range qualifiers {
			if qualifier != "" {
				loader.byName[qualifier] = append(loader.byName[qualifier], source)
			}
		}
	}
	if len(loader.bundles) == 0 {
		return nil
	}
	return loader
}

func (loader *bundleSourceLoader) withFallback(fallback SourceLoader) SourceLoader {
	if loader == nil {
		return fallback
	}
	return &bundleSourceLoader{byName: loader.byName, bundles: loader.bundles, fallback: fallback}
}

func (loader *bundleSourceLoader) Load(currentFile, target string) (data []byte, resolvedPath string, err error) {
	if qualifier, modulePath, ok := splitBundleSourceTarget(target); ok {
		matches := loader.byName[qualifier]
		switch len(matches) {
		case 0:
			return nil, "", fmt.Errorf("bundle source qualifier %q is not loaded; add @require with `as %s`", qualifier, qualifier)
		case 1:
			return matches[0].load(modulePath, true)
		default:
			return nil, "", fmt.Errorf("bundle source qualifier %q is ambiguous", qualifier)
		}
	}

	if source, currentPath, ok := loader.bundleForSource(currentFile); ok {
		modulePath := path.Clean(path.Join(path.Dir(currentPath), target))
		return source.load(modulePath, false)
	}
	fallback := loader.fallback
	if fallback == nil {
		fallback = fileSystemLoader{}
	}
	return fallback.Load(currentFile, target)
}

func (loader *bundleSourceLoader) bundleForSource(sourceURI string) (*bundleSource, string, bool) {
	var matched *bundleSource
	var matchedPath string
	longest := -1
	for _, source := range loader.bundles {
		prefix := bundleSourceURI + source.id + "/"
		if !strings.HasPrefix(sourceURI, prefix) || len(prefix) <= longest {
			continue
		}
		matched = source
		matchedPath = strings.TrimPrefix(sourceURI, prefix)
		longest = len(prefix)
	}
	return matched, matchedPath, matched != nil
}

func (source *bundleSource) load(modulePath string, exportedOnly bool) (data []byte, resolvedPath string, err error) {
	if strings.Contains(modulePath, `\`) || !fs.ValidPath(modulePath) || !strings.HasSuffix(modulePath, ".nflow") {
		return nil, "", fmt.Errorf("invalid bundle module path %q", modulePath)
	}
	if _, exported := source.exports[modulePath]; exportedOnly && !exported {
		return nil, "", fmt.Errorf("module %q is private or not exported by bundle %q", modulePath, source.id)
	}
	data, readErr := fs.ReadFile(source.fsys, modulePath)
	if readErr != nil {
		return nil, "", fmt.Errorf("exported module %q from bundle %q: %w", modulePath, source.id, readErr)
	}
	return data, bundleSourceURI + source.id + "/" + modulePath, nil
}

func splitBundleSourceTarget(target string) (qualifier, modulePath string, ok bool) {
	separator := strings.IndexByte(target, ':')
	if separator <= 0 || separator == len(target)-1 {
		return "", "", false
	}
	qualifier, modulePath = target[:separator], target[separator+1:]
	if strings.ContainsAny(qualifier, "/\\ \t\r\n") {
		return "", "", false
	}
	// Preserve Windows drive paths when Flow runs on a different platform.
	if len(qualifier) == 1 && isASCIIAlpha(qualifier[0]) && (strings.HasPrefix(modulePath, `/`) || strings.HasPrefix(modulePath, `\`)) {
		return "", "", false
	}
	return qualifier, modulePath, true
}

func isASCIIAlpha(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
