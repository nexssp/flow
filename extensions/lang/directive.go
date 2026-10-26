package lang

// LangBlock is the AST node produced by language keywords such as py, rs, and go.
//
// All aliases (py, python, python3) resolve to a LangBlock with the same ID
// and a Version reflecting the alias. The runtime never sees the tag — by
// the time we reach materialize/exec, the canonical ID is the only thing
// in scope.
//
//nolint:revive // LangBlock is the public AST node name; keeping the domain explicit avoids a breaking rename.
type LangBlock struct {
	ID         string // "python" — canonical, never the tag
	Version    string // "", "2", "3"
	Source     string // source code text
	Tag        string // source keyword, including ".file" when using a script file
	SourceFile string // absolute path for a file-backed script; empty for inline source
	// Pos is the source position supplied by the compiler parser.
	Pos any
}

// ParseTag converts a keyword like "py" or "python3" into a LangBlock ready
// for the runtime. The parser hook reads the keyword and source literal/block,
// then calls this function.
//
// ParseTag fails fast on one condition, surfaced to the user as a
// compile-time error rather than a runtime surprise:
//
//  1. Unknown keyword (not in the alias table).
//
// Interpreter presence is NOT checked here — that's preflight's job.
// ParseTag produces the AST node; the compiler calls Preflight.Run on the
// collected blocks before any runtime invocation. This separation means a
// flow with py and rs where rust is missing reports both checks in one
// pass, not whichever keyword happened to parse first.
//
// The caller is expected to wrap the returned error in
// compiler.SourceError(pos, ...) before it leaves the parser. We don't do
// that here because compiler.SourceError's signature varies across
// versions and we don't want this file coupled to it.
func ParseTag(tag, source string, pos any) (*LangBlock, error) {
	id, version, ok := resolveAlias(tag)
	if !ok {
		return nil, &UnknownTagError{Tag: tag}
	}
	if _, err := Lookup(id, version); err != nil {
		return nil, err
	}
	return &LangBlock{
		ID:      id,
		Version: version,
		Source:  source,
		Tag:     tag,
		Pos:     pos,
	}, nil
}

// ParseFileTag creates a language block that runs an existing source file.
// The caller resolves the path relative to the containing .nflow file and
// supplies the file contents so preflight can inspect dependency metadata.
func ParseFileTag(tag, sourceFile, source string, pos any) (*LangBlock, error) {
	block, err := ParseTag(tag, source, pos)
	if err != nil {
		return nil, err
	}
	block.Tag = tag + ".file"
	block.SourceFile = sourceFile
	return block, nil
}

// UnknownTagError is returned by ParseTag when the keyword is not in the alias
// table. The user sees this wrapped in a SourceError at the keyword.
type UnknownTagError struct{ Tag string }

func (e *UnknownTagError) Error() string { return "unknown language keyword " + e.Tag }

// --- Parser integration --------------------------------------------------
//
// The <keyword>"..." and <keyword> { ... } forms are implemented as
// KeywordPrimary extensions in primary.go.
//
// The @lang.runner and @lang.define file-scope directives are implemented
// as compiler.Directive extensions in lang_directive.go.
