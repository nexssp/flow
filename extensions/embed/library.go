// Package embed compiles static @embed declarations to typed file references or
// bounded directory asset maps.
package embed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"

	"github.com/nexssp/flow/compiler"
	fileext "github.com/nexssp/flow/extensions/files"
)

const (
	ID                 = "embed"
	DefaultProvider    = "flow.static"
	DefaultMaxBytes    = int64(16 << 20)
	refsMetaKey        = "embeds"
	directoriesMetaKey = "embed_directories"
)

// Resolver serves only assets declared during preprocessing. References are
// mapped through opaque IDs to validated relative paths and are integrity-checked
// on every open; bytes are bounded by maxBytes and are not retained.
type Resolver struct {
	fsys     fs.FS
	provider xfile.Provider
	maxBytes int64
	mu       sync.RWMutex
	assets   map[xfile.ID]assetReference
}

var _ xfile.Resolver = (*Resolver)(nil)

type assetPath string

type assetReference struct {
	path assetPath
	ref  xfile.Ref
}

var declarationPattern = regexp.MustCompile(`^@embed\s+([A-Za-z_][A-Za-z0-9_]*)\s+("(?:\\.|[^"\\])*")\s*$`)

func init() { compiler.Register(ID, Bundle) }

// Bundle creates the standard extension bundle. The optional "root" setting
// names a filesystem directory and "provider" selects the xfile provider label.
// "max_size" bounds each file, while "max_total_size" and "max_files" bound
// directory embeds. No implicit process working-directory root is used.
func Bundle(opts map[string]string) compiler.Bundle {
	root := strings.TrimSpace(opts["root"])
	provider := strings.TrimSpace(opts["provider"])
	if provider == "" {
		provider = DefaultProvider
	}
	limit := DefaultMaxBytes
	totalLimit := DefaultMaxTotalBytes
	maxFiles := DefaultMaxFiles
	if raw := strings.TrimSpace(opts["max_size"]); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			limit = parsed
		} else {
			return bundleWithFileResolver(nil, provider, limit, totalLimit, maxFiles, errors.New("embed: max_size must be a positive integer"))
		}
	}
	if raw := strings.TrimSpace(opts["max_total_size"]); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			totalLimit = parsed
		} else {
			return bundleWithFileResolver(nil, provider, limit, totalLimit, maxFiles, errors.New("embed: max_total_size must be a positive integer"))
		}
	}
	if raw := strings.TrimSpace(opts["max_files"]); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxFiles = parsed
		} else {
			return bundleWithFileResolver(nil, provider, limit, totalLimit, maxFiles, errors.New("embed: max_files must be a positive integer"))
		}
	}
	var fsys fs.FS
	var rootErr error
	if root != "" {
		fsys, rootErr = newRootFS(root)
	}
	return bundleWithFileResolver(fsys, provider, limit, totalLimit, maxFiles, rootErr)
}

// NewBundle binds the directive to an explicitly declared fs.FS root. This is
// the preferred integration point for embedded or otherwise sandboxed filesystems.
func NewBundle(fsys fs.FS, provider string, maxBytes int64) compiler.Bundle {
	bundle, _ := BundleWithFileResolver(fsys, provider, maxBytes)
	return bundle
}

// BundleWithFileResolver returns an embed bundle and its FS-backed resolver.
// The bundle registers the resolver so runner.BuildConfig composes it with
// other provider bindings automatically.
func BundleWithFileResolver(fsys fs.FS, provider string, maxBytes int64) (compiler.Bundle, *Resolver) {
	provider, maxBytes = normalizeResolverConfig(provider, maxBytes)
	resolver := newResolver(fsys, provider, maxBytes)
	bundle := newBundle(fsys, provider, maxBytes, DefaultMaxTotalBytes, DefaultMaxFiles, nil, resolver)
	return compiler.BundleWithFileResolver(bundle, xfile.Provider(provider), resolver), resolver
}

func newResolver(fsys fs.FS, provider string, maxBytes int64) *Resolver {
	provider, maxBytes = normalizeResolverConfig(provider, maxBytes)
	return &Resolver{
		fsys:     fsys,
		provider: xfile.Provider(provider),
		maxBytes: maxBytes,
		assets:   make(map[xfile.ID]assetReference),
	}
}

func normalizeResolverConfig(provider string, maxBytes int64) (normalizedProvider string, normalizedMaxBytes int64) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = DefaultProvider
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return provider, maxBytes
}

func bundleWithFileResolver(fsys fs.FS, provider string, maxBytes, maxTotalBytes int64, maxFiles int, rootErr error) compiler.Bundle {
	provider, maxBytes = normalizeResolverConfig(provider, maxBytes)
	resolver := newResolver(fsys, provider, maxBytes)
	bundle := newBundle(fsys, provider, maxBytes, maxTotalBytes, maxFiles, rootErr, resolver)
	return compiler.BundleWithFileResolver(bundle, xfile.Provider(provider), resolver)
}

func newBundle(fsys fs.FS, provider string, maxBytes, maxTotalBytes int64, maxFiles int, rootErr error, resolver *Resolver) compiler.Bundle {
	bundle := compiler.Bundle{
		ID:              ID,
		AcceptedOptions: []string{"root", "provider", "max_size", "max_total_size", "max_files"},
		Directives:      []compiler.Directive{directive(fsys, provider, maxBytes, maxTotalBytes, maxFiles, rootErr, resolver)},
		InheritScope:    inheritScope,
		OnPreprocess: func(_ map[string]any) compiler.PreprocessContributions {
			return compiler.PreprocessContributions{CompileOpts: []compiler.CompileOption{
				compiler.WithASTValidators(validateRefs),
			}}
		},
	}
	if rootFS, ok := fsys.(rootedFS); ok {
		bundle.Shutdowns = append(bundle.Shutdowns, func(context.Context) error { return rootFS.Close() })
	}
	return bundle
}

func inheritScope(parentMeta, childMeta map[string]any) error {
	if parentMeta == nil || childMeta == nil {
		return nil
	}
	if refs, ok := parentMeta[refsMetaKey].(map[string]xfile.Ref); ok {
		inherited := make(map[string]xfile.Ref, len(refs))
		maps.Copy(inherited, refs)
		if current, ok := childMeta[refsMetaKey].(map[string]xfile.Ref); ok {
			maps.Copy(inherited, current)
		}
		childMeta[refsMetaKey] = inherited
	}
	if directories, ok := parentMeta[directoriesMetaKey].(map[string]map[string][]byte); ok {
		inherited := make(map[string]map[string][]byte, len(directories))
		for name, files := range directories {
			copiedFiles := make(map[string][]byte, len(files))
			for path, data := range files {
				copiedFiles[path] = append([]byte(nil), data...)
			}
			inherited[name] = copiedFiles
		}
		if current, ok := childMeta[directoriesMetaKey].(map[string]map[string][]byte); ok {
			maps.Copy(inherited, current)
		}
		childMeta[directoriesMetaKey] = inherited
	}
	if namespace, ok := compiler.SymbolsFromMeta(parentMeta)[ID]; ok {
		namespaces, _ := childMeta["symbols"].(map[string]compiler.SymbolNamespace)
		if namespaces == nil {
			namespaces = make(map[string]compiler.SymbolNamespace)
		}
		inherited := make(map[string]any, len(namespace.Symbols))
		maps.Copy(inherited, namespace.Symbols)
		if current, exists := namespaces[ID]; exists {
			maps.Copy(inherited, current.Symbols)
		}
		namespaces[ID] = compiler.SymbolNamespace{Name: ID, Symbols: inherited}
		childMeta["symbols"] = namespaces
	}
	return nil
}

func directive(fsys fs.FS, provider string, maxBytes, maxTotalBytes int64, maxFiles int, rootErr error, resolver *Resolver) compiler.Directive {
	return compiler.Directive{
		Name:    "embed",
		Example: `@embed site "web/dist"`,
		Handler: func(_ context.Context, req compiler.DirectiveReq) (compiler.DirectiveRes, error) {
			pos := compiler.Position{File: req.File, Line: req.I + 1}
			line := strings.TrimSpace(req.Lines[req.I])
			matches := declarationPattern.FindStringSubmatch(line)
			if matches == nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, `@embed requires name "path" format`)
			}
			name := matches[1]
			assetPath, err := strconv.Unquote(matches[2])
			if err != nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed path is invalid: %v", err)
			}
			if pathErr := fileext.ValidatePath(assetPath); pathErr != nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed: %v", pathErr)
			}
			if rootErr != nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed filesystem root: %v", rootErr)
			}
			if fsys == nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed requires a declared Bundle fs.FS/root")
			}
			refs, _ := req.Out[refsMetaKey].(map[string]xfile.Ref)
			directories, _ := req.Out[directoriesMetaKey].(map[string]map[string][]byte)
			if _, duplicate := refs[name]; duplicate {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "duplicate @embed name %q", name)
			}
			if _, duplicate := directories[name]; duplicate {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "duplicate @embed name %q", name)
			}
			info, err := fs.Stat(fsys, assetPath)
			if err != nil {
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed %s: %v", name, err)
			}
			var symbol any
			switch {
			case info.IsDir():
				files, err := readDirectory(fsys, assetPath, maxBytes, maxTotalBytes, maxFiles)
				if err != nil {
					return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed %s: %v", name, err)
				}
				if directories == nil {
					directories = make(map[string]map[string][]byte)
				}
				directories[name] = files
				req.Out[directoriesMetaKey] = directories
				symbol = files
			case info.Mode().IsRegular():
				ref, err := makeRef(fsys, provider, assetPath, maxBytes)
				if err != nil {
					return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed %s: %v", name, err)
				}
				if err := resolver.register(ref, assetPath); err != nil {
					return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed %s: %v", name, err)
				}
				if refs == nil {
					refs = make(map[string]xfile.Ref)
				}
				refs[name] = ref
				req.Out[refsMetaKey] = refs
				symbol = ref
			default:
				return compiler.DirectiveRes{}, compiler.SourceError(pos, "@embed %s: path is not a regular file or directory", name)
			}

			symbols, _ := req.Out["symbols"].(map[string]compiler.SymbolNamespace)
			if symbols == nil {
				symbols = make(map[string]compiler.SymbolNamespace)
			}
			namespace, exists := symbols["embed"]
			if !exists {
				namespace = compiler.SymbolNamespace{Name: "embed", Symbols: make(map[string]any)}
			}
			if namespace.Symbols == nil {
				namespace.Symbols = make(map[string]any)
			}
			namespace.Symbols[name] = symbol
			symbols["embed"] = namespace
			req.Out["symbols"] = symbols
			return compiler.DirectiveRes{Next: req.I + 1}, nil
		},
	}
}

func makeRef(fsys fs.FS, provider, name string, maxBytes int64) (xfile.Ref, error) {
	data, err := readAsset(fsys, name, maxBytes)
	if err != nil {
		return xfile.Ref{}, err
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	idSum := sha256.Sum256([]byte(name + "\x00" + digest))
	return xfile.Ref{
		Provider:       xfile.Provider(provider),
		ID:             xfile.ID(hex.EncodeToString(idSum[:])),
		Version:        xfile.Version(digest),
		ExpectedSize:   xfile.ExpectedSize(len(data)),
		ExpectedDigest: xfile.ExpectedDigest(digest),
	}, nil
}

func readAsset(fsys fs.FS, name string, maxBytes int64) (data []byte, resultErr error) {
	if fsys == nil {
		return nil, errors.New("embed: filesystem is nil")
	}
	if maxBytes <= 0 {
		return nil, errors.New("embed: size limit must be positive")
	}
	if err := fileext.ValidatePath(name); err != nil {
		return nil, err
	}
	if err := rejectSymlinkComponents(fsys, name); err != nil {
		return nil, err
	}
	file, err := fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", name, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close %q: %w", name, closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", name)
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("%q exceeds size limit (%d bytes)", name, maxBytes)
	}
	readLimit := maxBytes
	if maxBytes < int64(^uint64(0)>>1) {
		readLimit++
	}
	data, err = io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", name, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%q exceeds size limit (%d bytes)", name, maxBytes)
	}
	return data, nil
}

// rejectSymlinkComponents enforces the strongest check available through fs.FS.
// OS roots use rootedFS below, which resolves every candidate against a pinned
// canonical root. ReadLinkFS implementations are rejected if any component is
// a symlink; immutable virtual filesystems such as embed.FS have no symlinks.
func rejectSymlinkComponents(fsys fs.FS, name string) error {
	reader, ok := fsys.(fs.ReadLinkFS)
	if !ok {
		return nil
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		candidate := strings.Join(parts[:i+1], "/")
		if _, err := reader.ReadLink(candidate); err == nil {
			return fmt.Errorf("symlink path component %q is not allowed", candidate)
		} else if !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, fs.ErrNotExist) {
			// A filesystem that cannot establish that the component is not a link
			// is not suitable as a compile-time embed root. Do not treat arbitrary
			// PathError values as proof that the component is not a symlink.
			return fmt.Errorf("cannot verify path component %q: %w", candidate, err)
		}
	}
	return nil
}

func newRootFS(root string) (fs.FS, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	rootHandle, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, err
	}
	info, err := rootHandle.Stat(".")
	if err != nil {
		return nil, closeRootOnError(rootHandle, err)
	}
	if !info.IsDir() {
		return nil, closeRootOnError(rootHandle, fmt.Errorf("%q is not a directory", root))
	}
	return rootedFS{root: rootHandle}, nil
}

func closeRootOnError(root *os.Root, cause error) error {
	if closeErr := root.Close(); closeErr != nil {
		return errors.Join(cause, fmt.Errorf("close embed root: %w", closeErr))
	}
	return cause
}

type rootedFS struct{ root *os.Root }

func (r rootedFS) Open(name string) (fs.File, error) {
	if err := fileext.ValidatePath(name); err != nil {
		return nil, err
	}
	// os.Root resolves symlinks while enforcing the directory boundary, avoiding
	// the check-then-open race of EvalSymlinks followed by os.Open.
	return r.root.Open(name)
}

func (r rootedFS) Close() error {
	return r.root.Close()
}

func (r *Resolver) register(ref xfile.Ref, name string) error {
	if r == nil {
		return xerr.Internal("embed: resolver is nil")
	}
	if ref.Provider != r.provider || ref.ID == "" {
		return xerr.Validation("embed: resolver received an invalid reference")
	}
	if err := fileext.ValidatePath(name); err != nil {
		return xerr.Validation("embed: invalid asset path", err)
	}
	if ref.Version == "" || ref.ExpectedDigest == "" || ref.ExpectedSize < 0 {
		return xerr.Validation("embed: reference is missing integrity metadata")
	}
	asset := assetReference{path: assetPath(name), ref: ref}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.assets[ref.ID]; ok {
		if existing != asset {
			return xerr.Conflict("embed: opaque reference ID collision")
		}
		return nil
	}
	r.assets[ref.ID] = asset
	return nil
}

func (r *Resolver) Open(ctx context.Context, ref xfile.Ref) (io.ReadCloser, xfile.Info, error) {
	asset, err := r.assetFor(ctx, ref)
	if err != nil {
		return nil, xfile.Info{}, err
	}
	return r.openAsset(ctx, ref, asset)
}

func (r *Resolver) assetFor(ctx context.Context, ref xfile.Ref) (assetReference, error) {
	if r == nil || r.fsys == nil {
		return assetReference{}, xerr.Validation("embed: filesystem is unavailable")
	}
	if ctx == nil {
		return assetReference{}, xerr.BadRequest("embed: context is nil")
	}
	if _, err := xfile.ScopeFromContext(ctx); err != nil {
		return assetReference{}, err
	}
	if err := ctx.Err(); err != nil {
		return assetReference{}, xerr.Canceled("embed: file open canceled", err)
	}
	if ref.Provider != r.provider {
		return assetReference{}, xerr.NotFound("embed: file provider is not registered")
	}
	if ref.ExpectedSize < 0 {
		return assetReference{}, xerr.Validation("embed: expected size cannot be negative")
	}
	r.mu.RLock()
	asset, ok := r.assets[ref.ID]
	r.mu.RUnlock()
	if !ok {
		return assetReference{}, xerr.NotFound("embed: declared asset was not found")
	}
	if err := fileext.ValidatePath(string(asset.path)); err != nil {
		return assetReference{}, xerr.Validation("embed: stored asset path is invalid", err)
	}
	if ref.Version != "" && ref.Version != asset.ref.Version {
		return assetReference{}, xerr.Conflict("embed: reference version does not match the declared asset")
	}
	if ref.ExpectedSize != 0 && ref.ExpectedSize != asset.ref.ExpectedSize {
		return assetReference{}, xerr.Conflict("embed: expected size does not match the declared asset")
	}
	if ref.ExpectedDigest != "" && ref.ExpectedDigest != asset.ref.ExpectedDigest {
		return assetReference{}, xerr.Conflict("embed: expected digest does not match the declared asset")
	}
	return asset, nil
}

func (r *Resolver) openAsset(ctx context.Context, ref xfile.Ref, asset assetReference) (io.ReadCloser, xfile.Info, error) {
	data, err := readAsset(r.fsys, string(asset.path), r.maxBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, xfile.Info{}, xerr.Conflict("embed: declared asset is no longer available", err)
		}
		return nil, xfile.Info{}, xerr.Unavailable("embed: failed to read declared asset", err)
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if int64(len(data)) != int64(asset.ref.ExpectedSize) || digest != string(asset.ref.ExpectedDigest) || digest != string(asset.ref.Version) {
		return nil, xfile.Info{}, xerr.Conflict("embed: declared asset changed since compilation")
	}
	if ref.ExpectedSize > 0 && ref.ExpectedSize != xfile.ExpectedSize(len(data)) {
		return nil, xfile.Info{}, xerr.Conflict("embed: opened asset size does not match the reference")
	}
	if ref.ExpectedDigest != "" && ref.ExpectedDigest != xfile.ExpectedDigest(digest) {
		return nil, xfile.Info{}, xerr.Conflict("embed: opened asset digest does not match the reference")
	}
	if err := ctx.Err(); err != nil {
		return nil, xfile.Info{}, xerr.Canceled("embed: file open canceled", err)
	}
	info := xfile.Info{
		Provider: r.provider,
		ID:       ref.ID,
		Version:  xfile.Version(digest),
		Size:     int64(len(data)),
		Digest:   digest,
	}
	return io.NopCloser(bytes.NewReader(data)), info, nil
}

func validateRefs(ast compiler.Expr, meta map[string]any) error {
	refs, _ := meta[refsMetaKey].(map[string]xfile.Ref)
	directories, _ := meta[directoriesMetaKey].(map[string]map[string][]byte)
	if namespace, ok := compiler.SymbolsFromMeta(meta)["embed"]; ok {
		for name, value := range namespace.Symbols {
			switch asset := value.(type) {
			case xfile.Ref:
				if refs == nil {
					refs = make(map[string]xfile.Ref)
				}
				refs[name] = asset
			case map[string][]byte:
				if directories == nil {
					directories = make(map[string]map[string][]byte)
				}
				directories[name] = asset
			}
		}
	}
	validator := refValidator{refs: refs, directories: directories}
	if err := validator.walk(ast); err != nil {
		return err
	}
	assertions, ok := meta["asserts"].([]string)
	if !ok {
		return nil
	}
	for _, expression := range assertions {
		if err := validateExpression(expression, validator.validateExpressionRef); err != nil {
			return err
		}
	}
	return nil
}

type refValidator struct {
	refs        map[string]xfile.Ref
	directories map[string]map[string][]byte
}

func (v refValidator) validateRef(name string) error {
	if _, ok := v.refs[name]; ok {
		return nil
	}
	if _, ok := v.directories[name]; ok {
		return nil
	}
	return xerr.Validation(fmt.Sprintf("unknown embed.%s reference", name))
}

func (v refValidator) validateExpressionRef(name string) error {
	if _, isDirectory := v.directories[name]; isDirectory {
		return nil
	}
	if err := v.validateRef(name); err != nil {
		return err
	}
	return xerr.Validation(fmt.Sprintf("embed.%s must be passed as a typed ( ... ) value", name))
}

func (v refValidator) walk(node compiler.Expr) error {
	if node == nil {
		return nil
	}
	if err := v.validateNode(node); err != nil {
		return err
	}
	for _, child := range node.Children() {
		if err := v.walk(child); err != nil {
			return err
		}
	}
	return nil
}

func (v refValidator) validateNode(node compiler.Expr) error {
	switch current := node.(type) {
	case *compiler.Atom:
		return v.validateAtom(current)
	case *compiler.AssertExpr:
		if err := validateExpression(current.Condition, v.validateExpressionRef); err != nil {
			return err
		}
		if current.MessageExpression != "" {
			return validateExpression(current.MessageExpression, v.validateExpressionRef)
		}
		return nil
	case *compiler.LoopExpr:
		return validateExpression(current.Until, v.validateExpressionRef)
	case *compiler.ProjectionExpr:
		return validateExpression(current.Raw, v.validateExpressionRef)
	default:
		return nil
	}
}

func (v refValidator) validateAtom(atom *compiler.Atom) error {
	for _, value := range atom.Args {
		if err := validateValue(value, v.validateRef); err != nil {
			return err
		}
	}
	for _, expression := range atom.Params {
		if err := validateExpression(expression, v.validateExpressionRef); err != nil {
			return err
		}
	}
	for _, modifier := range atom.Modifiers {
		if err := validateExpression(modifier, v.validateExpressionRef); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(value *compiler.Value, validate func(string) error) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case compiler.ValueBare:
		if name, ok := strings.CutPrefix(value.Str, "embed."); ok {
			return validate(name)
		}
	case compiler.ValueMap:
		for _, entry := range value.Map {
			if err := validateValue(entry.Value, validate); err != nil {
				return err
			}
		}
	case compiler.ValueSlice:
		for _, item := range value.Slice {
			if err := validateValue(item, validate); err != nil {
				return err
			}
		}
	case compiler.ValueString, compiler.ValueNumber, compiler.ValueBool, compiler.ValueNull, compiler.ValueRef, compiler.ValueTyped, compiler.ValueExpression:
		return nil
	default:
		return nil
	}
	return nil
}

func validateExpression(text string, validate func(string) error) error {
	for index := 0; index < len(text); {
		next, err := scanExpression(text, index, validate)
		if err != nil {
			return err
		}
		index = next
	}
	return nil
}

func scanExpression(text string, index int, validate func(string) error) (int, error) {
	switch text[index] {
	case '\'', '"', '`':
		return skipQuoted(text, index), nil
	case '#':
		return skipComment(text, index), nil
	}
	if _, ok := strings.CutPrefix(text[index:], "//"); ok {
		return skipComment(text, index), nil
	}
	start, ok := expressionReferenceStart(text, index)
	if !ok {
		return index + 1, nil
	}
	end := identifierEnd(text, start)
	if end == start {
		return index + 1, nil
	}
	if err := validate(text[start:end]); err != nil {
		return 0, err
	}
	return end, nil
}

func expressionReferenceStart(text string, index int) (int, bool) {
	if index > 0 && isIdentifierByte(text[index-1]) {
		return 0, false
	}
	if _, ok := strings.CutPrefix(text[index:], "embed."); !ok {
		return 0, false
	}
	return index + len("embed."), true
}

func identifierEnd(text string, start int) int {
	if start >= len(text) || !isIdentifierStart(text[start]) {
		return start
	}
	end := start + 1
	for end < len(text) && isIdentifierByte(text[end]) {
		end++
	}
	return end
}

func skipQuoted(text string, start int) int {
	quote := text[start]
	for index := start + 1; index < len(text); {
		if quote != '`' && text[index] == '\\' {
			if index+1 == len(text) {
				return len(text)
			}
			index += 2
			continue
		}
		if text[index] == quote {
			return index + 1
		}
		index++
	}
	return len(text)
}

func skipComment(text string, start int) int {
	for start < len(text) && text[start] != '\n' {
		start++
	}
	return start
}

func isIdentifierStart(b byte) bool { return b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
func isIdentifierByte(b byte) bool  { return isIdentifierStart(b) || b >= '0' && b <= '9' }
