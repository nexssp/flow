package cli

import (
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/kernel/xfs"
)

func (s *flowLanguageServer) handleWorkspaceSymbol(message lspMessage) error {
	var params struct {
		Query string `json:"query"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	return s.respond(message.ID, s.workspaceSymbols(params.Query))
}

func (s *flowLanguageServer) workspaceSymbols(query string) []map[string]any {
	sources := s.workspaceSymbolSources()
	query = strings.ToLower(query)
	type workspaceSymbol struct {
		name       string
		kind       any
		uri        string
		rangeValue lspRange
	}
	symbols := make([]workspaceSymbol, 0)
	uris := make([]string, 0, len(sources))
	for uri := range sources {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		var documentSymbols []map[string]any
		if openSource, open := s.documents[uri]; open && openSource == sources[uri] {
			entry := s.schemaCacheEntry(uri, openSource)
			documentSymbols = lspDocumentSymbolsFromIndex(openSource, entry.sourceIndex)
		} else {
			index, _, _, _, err := s.sourceIndexAndMetadata(uri, sources[uri])
			if err != nil {
				slog.Debug("workspace symbol source index is partial", "uri", uri, "error", err)
			}
			documentSymbols = lspDocumentSymbolsFromIndex(sources[uri], index)
		}
		for _, symbol := range documentSymbols {
			name, _ := symbol["name"].(string)
			if !strings.Contains(strings.ToLower(name), query) {
				continue
			}
			selectionRange, ok := symbol["selectionRange"].(lspRange)
			if !ok {
				continue
			}
			symbols = append(symbols, workspaceSymbol{
				name:       name,
				kind:       symbol["kind"],
				uri:        uri,
				rangeValue: selectionRange,
			})
		}
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].name != symbols[j].name {
			return symbols[i].name < symbols[j].name
		}
		return symbols[i].uri < symbols[j].uri
	})
	results := make([]map[string]any, 0, len(symbols))
	for _, symbol := range symbols {
		results = append(results, map[string]any{
			"name": symbol.name,
			"kind": symbol.kind,
			"location": map[string]any{
				"uri":       symbol.uri,
				lspRangeKey: symbol.rangeValue,
			},
		})
	}
	return results
}

func (s *flowLanguageServer) workspaceSymbolSources() map[string]string {
	sources := make(map[string]string, len(s.documents))
	openByPath := make(map[string]struct{}, len(s.documents))
	openURIs := make([]string, 0, len(s.documents))
	for uri := range s.documents {
		openURIs = append(openURIs, uri)
	}
	sort.Strings(openURIs)
	for _, uri := range openURIs {
		sources[uri] = s.documents[uri]
		if path := lspFileURIPath(uri); path != "" {
			openByPath[lspSchemaPathKey(path)] = struct{}{}
		}
	}
	if s.rootPath != "" {
		s.addWorkspaceSymbolFiles(s.rootPath, sources, openByPath)
	}
	return sources
}

func (s *flowLanguageServer) addWorkspaceSymbolFiles(configuredRoot string, sources map[string]string, openByPath map[string]struct{}) {
	rootPath, err := filepath.Abs(configuredRoot)
	if err != nil {
		slog.Debug("workspace symbol root is unavailable", "path", configuredRoot, "error", err)
		return
	}
	root, err := xfs.OpenRoot(rootPath)
	if err != nil {
		slog.Debug("workspace symbol root is unavailable", "path", rootPath, "error", err)
		return
	}
	defer func() {
		if err := root.Close(); err != nil {
			slog.Debug("workspace symbol root close failed", "path", rootPath, "error", err)
		}
	}()

	rootFS := root.FS()
	walkErr := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Debug("workspace symbol path is unavailable", "path", path, "error", walkErr)
			return nil
		}
		if entry.IsDir() {
			if path != "." {
				name := entry.Name()
				_, excluded := lintDiscoveryExcludedDirs[name]
				if excluded || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return fs.SkipDir
				}
			}
			return nil
		}
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".nflow") || strings.HasPrefix(name, "_") {
			return nil
		}
		filePath := filepath.Join(rootPath, filepath.FromSlash(path))
		if _, open := openByPath[lspSchemaPathKey(filePath)]; open {
			return nil
		}
		content, readErr := fs.ReadFile(rootFS, path)
		if readErr != nil {
			slog.Debug("workspace symbol source is unavailable", "path", filePath, "error", readErr)
			return nil
		}
		uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filePath)}).String()
		sources[uri] = string(content)
		return nil
	})
	if walkErr != nil {
		slog.Debug("workspace symbol scan stopped", "path", rootPath, "error", walkErr)
	}
}
