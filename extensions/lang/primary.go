// Package lang — parser integration.
package lang

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

// langTagPrimary is one KeywordPrimary per runner alias. Each dispatches to
// the same parser, which reads inline source, a raw block, or a script path.
type langTagPrimary struct {
	keyword  string // "py", "python3", "py.file", "rs", ...
	tag      string // runner alias without the .file suffix
	fileMode bool
}

func (p langTagPrimary) Name() string    { return "lang." + p.keyword }
func (p langTagPrimary) Keyword() string { return p.keyword }

// MergeWith allows the same language primary to be inherited and contributed
// again by nested pipeline compilation. A different extension claiming the
// same keyword remains a loud conflict.
func (p langTagPrimary) MergeWith(older compiler.PrimaryExtension) compiler.PrimaryExtension {
	previous, ok := older.(langTagPrimary)
	if !ok || previous.keyword != p.keyword || previous.tag != p.tag || previous.fileMode != p.fileMode {
		panic("lang: cannot merge a different primary into keyword " + p.keyword)
	}
	return p
}

// Parse accepts three forms:
//
//	py"print(1)"             — inline string literal
//	py { ... }               — raw inline source block
//	py.file "analysis.py"    — existing script beside the .nflow file
//	py.file "analysis.py" :autocreate — create a missing script from its runner template
func (p langTagPrimary) Parse(c *compiler.Parser) (compiler.Expr, error) {
	startPos := c.Position()
	c.Advance() // consume the keyword

	cur := c.Current()
	if p.fileMode {
		if cur.Type != compiler.TokString {
			return nil, c.Fail(cur.Line, "%s: expected a quoted script path", p.keyword)
		}
		if cur.Unterminated {
			return nil, c.Fail(cur.Line, "%s: unterminated script path", p.keyword)
		}
		if strings.TrimSpace(cur.Lit) == "" {
			return nil, c.Fail(cur.Line, "%s: script path cannot be empty", p.keyword)
		}
		if strings.ContainsAny(cur.Lit, "\r\n") {
			return nil, c.Fail(cur.Line, "%s: script path cannot contain a newline", p.keyword)
		}
		c.Advance() // consume the path so an optional :autocreate can follow
		autocreate, err := parseAutocreateOption(c)
		if err != nil {
			return nil, compiler.SourceError(startPos, "%s: %v", p.keyword, err)
		}
		filePath, err := resolveScriptPath(c.File(), cur.Lit)
		if err != nil {
			return nil, compiler.SourceError(startPos, "%s: resolve script path: %v", p.keyword, err)
		}
		source, err := os.ReadFile(filePath)
		if err != nil && autocreate && errors.Is(err, os.ErrNotExist) {
			template, templateErr := runnerFileTemplate(p.tag)
			if templateErr != nil {
				return nil, compiler.SourceError(startPos, "%s: %v", p.keyword, templateErr)
			}
			source, err = createScriptFile(filePath, template)
			if err != nil {
				return nil, compiler.SourceError(startPos, "%s: create script %q: %v", p.keyword, cur.Lit, err)
			}
		}
		if err != nil {
			return nil, compiler.SourceError(startPos, "%s: read script %q: %v", p.keyword, cur.Lit, err)
		}
		return parseLangFileIntoBlock(p.tag, filePath, string(source), startPos)
	}

	//nolint:exhaustive // Only string and raw-block tokens are valid here; the default reports every other token.
	switch cur.Type {
	case compiler.TokString:
		// Inline form: py"print(1)"
		if cur.Unterminated {
			return nil, c.Fail(cur.Line, "%s: unterminated string", p.keyword)
		}
		source := cur.Lit
		c.Advance()
		return parseLangTagIntoBlock(p.keyword, source, startPos)
	case compiler.TokLBrace:
		// Block form: py { ... }
		readBody := readBlockBody
		if isPythonAlias(p.tag) {
			readBody = readPythonBlockBody
		}
		source, err := readBody(c)
		if err != nil {
			return nil, err
		}
		return parseLangTagIntoBlock(p.keyword, source, startPos)
	default:
		return nil, c.Fail(cur.Line, "%s: expected string literal or block, got %s",
			p.keyword, cur.Type)
	}
}

func parseAutocreateOption(c *compiler.Parser) (bool, error) {
	if c.Current().Type != compiler.TokColon || c.Peek(1).Type != compiler.TokIdent || c.Peek(1).Lit != "autocreate" {
		return false, nil
	}
	c.Advance() // :
	c.Advance() // autocreate
	if c.Current().Type != compiler.TokEquals {
		return true, nil
	}
	c.Advance() // =
	if c.Current().Type != compiler.TokIdent {
		return false, c.Fail(c.Current().Line, "expected true or false after :autocreate=")
	}
	switch strings.ToLower(c.Current().Lit) {
	case "true":
		c.Advance()
		return true, nil
	case "false":
		c.Advance()
		return false, nil
	default:
		return false, c.Fail(c.Current().Line, "expected true or false after :autocreate=")
	}
}

func runnerFileTemplate(tag string) (string, error) {
	id, version, ok := resolveAlias(tag)
	if !ok {
		return "", fmt.Errorf("unknown language keyword %q", tag)
	}
	language, err := Lookup(id, version)
	if err != nil {
		return "", err
	}
	if language.spec == nil || strings.TrimSpace(language.spec.FileTemplate) == "" {
		return "", fmt.Errorf("runner %s does not declare a file_template for JSON-in/JSON-out autocreate", registryKey(id, version))
	}
	return language.spec.FileTemplate, nil
}

func createScriptFile(filePath, source string) ([]byte, error) {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ReadFile(filePath)
		}
		return nil, err
	}
	if _, err := file.WriteString(source); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(filePath)
		return nil, err
	}
	return []byte(source), nil
}

// resolveScriptPath anchors relative paths beside the .nflow file. An empty
// source filename (for stdin or an in-memory flow) uses the current folder.
func resolveScriptPath(sourceFile, scriptPath string) (string, error) {
	if !filepath.IsAbs(scriptPath) {
		baseDir := "."
		if sourceFile != "" {
			baseDir = filepath.Dir(sourceFile)
		}
		scriptPath = filepath.Join(baseDir, scriptPath)
	}
	return filepath.Abs(filepath.Clean(scriptPath))
}

// parseLangTagIntoBlock resolves the alias, builds a LangBlock, and wraps
// it as a compiler.Expr. Errors are SourceError via c.Fail's positioning.
func parseLangTagIntoBlock(tag, source string, pos compiler.Position) (compiler.Expr, error) {
	block, err := ParseTag(tag, source, pos)
	if err != nil {
		return nil, err
	}
	return &langBlockExpr{block: block, pos: pos}, nil
}

func parseLangFileIntoBlock(tag, filePath, source string, pos compiler.Position) (compiler.Expr, error) {
	block, err := ParseFileTag(tag, filePath, source, pos)
	if err != nil {
		return nil, err
	}
	return &langBlockExpr{block: block, pos: pos}, nil
}

// readBlockBody scans raw text between { and }, depth-aware. Mirrors the
// loop extension's until(...) scanning. The body survives verbatim.
func readBlockBody(c *compiler.Parser) (string, error) {
	startTok := c.Current()
	if startTok.Type != compiler.TokLBrace {
		return "", c.Fail(startTok.Line, "expected '{' to open block")
	}
	bodyStart := startTok.Offset + 1
	c.Advance() // consume '{'

	depth := 1
	for c.Current().Type != compiler.TokEOF {
		//nolint:exhaustive // Only braces affect block depth; all other tokens are advanced over unchanged.
		switch c.Current().Type {
		case compiler.TokLBrace:
			depth++
		case compiler.TokRBrace:
			depth--
			if depth == 0 {
				body := c.Src()[bodyStart:c.Current().Offset]
				c.Advance() // consume '}'
				return body, nil
			}
		}
		c.Advance()
	}
	return "", c.Fail(startTok.Line, "unterminated block (missing '}')")
}

func isPythonAlias(tag string) bool {
	for _, alias := range aliases {
		if alias.Tag == tag {
			return alias.ID == "python"
		}
	}
	return false
}

// readPythonBlockBody scans Python source directly, rather than relying on
// Flow's host lexer for comment and string boundaries. This keeps a brace in a
// #comment, ordinary/triple-quoted string, or f-string from ending the block.
func readPythonBlockBody(c *compiler.Parser) (string, error) {
	startTok := c.Current()
	if startTok.Type != compiler.TokLBrace {
		return "", c.Fail(startTok.Line, "expected '{' to open block")
	}
	source := c.Src()
	bodyStart := startTok.Offset + 1
	depth := 1
	for offset := bodyStart; offset < len(source); {
		switch source[offset] {
		case '#':
			for offset < len(source) && source[offset] != '\n' {
				offset++
			}
		case '\'', '"':
			offset = skipPythonString(source, offset)
		case '{':
			depth++
			offset++
		case '}':
			depth--
			if depth == 0 {
				for c.Current().Type != compiler.TokEOF && c.Current().Offset < offset {
					c.Advance()
				}
				if c.Current().Type != compiler.TokRBrace || c.Current().Offset != offset {
					return "", c.Fail(startTok.Line, "could not align Python block closing brace")
				}
				body := source[bodyStart:offset]
				c.Advance()
				return body, nil
			}
			offset++
		default:
			offset++
		}
	}
	return "", c.Fail(startTok.Line, "unterminated block (missing '}')")
}

func skipPythonString(source string, start int) int {
	quote := source[start]
	triple := start+2 < len(source) && source[start+1] == quote && source[start+2] == quote
	if triple {
		start += 3
	} else {
		start++
	}
	for start < len(source) {
		if source[start] == '\\' {
			if start+1 < len(source) {
				start += 2
			} else {
				start++
			}
			continue
		}
		if source[start] == quote {
			if !triple {
				return start + 1
			}
			if start+2 < len(source) && source[start+1] == quote && source[start+2] == quote {
				return start + 3
			}
		}
		start++
	}
	return len(source)
}

// langBlockExpr adapts our *LangBlock to the compiler.Expr interface.
// Build lowers the LangBlock into a kernel action that calls runLangBlock.
type langBlockExpr struct {
	block *LangBlock
	pos   compiler.Position
}

func (e *langBlockExpr) Node()                     {}
func (e *langBlockExpr) Children() []compiler.Expr { return nil }

// Analyze is a no-op for now. The capability check (is the interpreter
// installed?) happens in Preflight, not here.
func (e *langBlockExpr) Analyze(compiler.CapabilityResolver) error { return nil }

// Build lowers the LangBlock into a kernel action.
func (e *langBlockExpr) Build(_ context.Context, _ *compiler.BuildContext) (action.AnyAction, error) {
	block := e.block
	return action.New("lang."+block.ID, func(ctx context.Context, in any) (any, error) {
		return runLangBlock(ctx, block, in)
	}).Build(), nil
}

// keywordPrimaries returns one KeywordPrimary per registered runner alias.
// Each alias also gets a .file form, such as py.file "analysis.py".
func keywordPrimaries() []compiler.PrimaryExtension {
	out := make([]compiler.PrimaryExtension, 0, len(aliases)*2)
	for _, a := range aliases {
		out = append(out,
			langTagPrimary{keyword: a.Tag, tag: a.Tag},
			langTagPrimary{keyword: a.Tag + ".file", tag: a.Tag, fileMode: true},
		)
	}
	return out
}
