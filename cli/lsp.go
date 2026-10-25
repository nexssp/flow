package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/nexssp/flow/compiler"
	flowformatter "github.com/nexssp/flow/formatter"
	"github.com/nexssp/flow/runner"
)

const lspSource = "nflow"

type flowLanguageServer struct {
	ctx       context.Context
	inv       *invocation
	rootPath  string
	documents map[string]string
	out       io.Writer
}

type lspMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type lspTextDocument struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type lspTextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}

type lspSnippet struct {
	label      string
	insertText string
	detail     string
}

var flowSnippets = []lspSnippet{
	{label: "flow-pipe", insertText: "source.node\n-> target.node", detail: "Sequential pipeline"},
	{label: "flow-args", insertText: "action.node @{ value: ${1:.input} }", detail: "Structured action arguments"},
	{label: "flow-parallel", insertText: "parallel {\n  first: ${1:action.one},\n  second: ${2:action.two},\n}", detail: "Named parallel fan-out"},
	{label: "flow-race", insertText: "race {\n  ${1:action.one},\n  ${2:action.two},\n}", detail: "First successful alternative"},
	{label: "flow-match", insertText: "match {\n  ${1:.condition} -> ${2:action.one},\n  _ -> ${3:action.other}\n}", detail: "Match input against ordered arms"},
	{label: "flow-fallback", insertText: "( primary.node || fallback.node )", detail: "Fallback chain"},
	{label: "flow-if", insertText: "gate ? target.node", detail: "Conditional node"},
	{label: "flow-projection", insertText: "{ field: .input }", detail: "Inline projection"},
	{label: "flow-loop", insertText: "loop( body.node ) until( condition )", detail: "Bounded loop"},
	{label: "flow-node", insertText: "namespace.node :timeout=10s :retry=2", detail: "Configured action node"},
	{label: "flow-pipeline", insertText: "@pipeline name\n  :timeout=10s\n  :retry=2\n\n  ${1:source.node}\n@end", detail: "Named pipeline with multiline modifiers"},
	{label: "flow-assert", insertText: "@assert {\n  result.ok == true\n}", detail: "Post-run assertion block"},
	{label: "flow-assert-expression", insertText: "@assert: ${1:result.ok == true}", detail: "Single-expression assertion (Flow v0.42.0)"},
	{label: "flow-assert-inline", insertText: "@assert: { ${1:result.ok == true}, ${2:result.count > 0} }", detail: "Compact assertion list (Flow v0.42.0)"},
	{label: "flow-assert-call", insertText: "assert(${1:condition}, \"${2:failure message}\")", detail: "Runtime assertion expression"},
}

func runLSP(ctx context.Context, inv *invocation, args []string) int {
	if wantsHelp(args) {
		PrintCommandHelp(os.Stdout, "lsp")
		return 0
	}
	if len(args) != 0 {
		return fatalf("usage: nflow lsp (stdio)")
	}
	server := newFlowLanguageServer(ctx, inv)
	if err := server.serve(os.Stdin, os.Stdout); err != nil {
		return fatalf("nflow lsp: %v", err)
	}
	return 0
}

func newFlowLanguageServer(ctx context.Context, inv *invocation) *flowLanguageServer {
	return &flowLanguageServer{
		ctx:       ctx,
		inv:       inv,
		documents: make(map[string]string),
	}
}

func (s *flowLanguageServer) serve(input io.Reader, output io.Writer) error {
	if input == nil {
		return errors.New("nflow lsp: input is unavailable")
	}
	if output == nil {
		return errors.New("nflow lsp: output is unavailable")
	}
	reader := bufio.NewReader(input)
	s.out = output
	for {
		body, err := readLSPFrame(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var message lspMessage
		if err := json.Unmarshal(body, &message); err != nil {
			if writeErr := s.respondError(json.RawMessage("null"), -32700, "parse error: "+err.Error()); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err := s.handle(message); err != nil {
			return err
		}
		if message.Method == "exit" {
			return nil
		}
	}
}

func (s *flowLanguageServer) handle(message lspMessage) error {
	switch message.Method {
	case "initialize":
		return s.handleInitialize(message)
	case "initialized", "$/cancelRequest", "exit":
		return nil
	case "shutdown":
		if len(message.ID) != 0 {
			return s.respond(message.ID, nil)
		}
		return nil
	case "textDocument/didOpen":
		return s.handleDidOpen(message)
	case "textDocument/didChange":
		return s.handleDidChange(message)
	case "textDocument/didClose":
		return s.handleDidClose(message)
	case "textDocument/completion":
		return s.handleCompletion(message)
	case "textDocument/hover":
		return s.handleHover(message)
	case "textDocument/formatting":
		return s.handleFormatting(message)
	default:
		if len(message.ID) != 0 {
			return s.respondError(message.ID, -32601, "method not found: "+message.Method)
		}
		return nil
	}
}

func (s *flowLanguageServer) handleInitialize(message lspMessage) error {
	var params struct {
		RootURI          string `json:"rootUri"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if len(message.ID) == 0 {
		return s.invalidParams(message, errors.New("initialize must be a request with an id"))
	}
	s.rootPath = lspFileURIPath(params.RootURI)
	if s.rootPath == "" {
		for _, folder := range params.WorkspaceFolders {
			if s.rootPath = lspFileURIPath(folder.URI); s.rootPath != "" {
				break
			}
		}
	}
	return s.respond(message.ID, map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync":           map[string]any{"openClose": true, "change": 1},
			"documentFormattingProvider": true,
			"completionProvider":         map[string]any{"triggerCharacters": []string{"@", ":", ".", " ", "-", "|", "&", "?", "{"}},
			"hoverProvider":              true,
		},
		"serverInfo": map[string]string{"name": "nflow", versionCommand: compiler.Version},
	})
}

func (s *flowLanguageServer) handleDidOpen(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocument `json:"textDocument"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" {
		return s.invalidParams(message, errors.New("textDocument.uri is required"))
	}
	uri := params.TextDocument.URI
	s.documents[uri] = params.TextDocument.Text
	return s.publishDiagnostics(uri, s.compileDiagnostics(uri, params.TextDocument.Text))
}

func (s *flowLanguageServer) handleDidChange(message lspMessage) error {
	var params struct {
		TextDocument   *lspTextDocumentIdentifier `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" {
		return s.invalidParams(message, errors.New("textDocument.uri is required"))
	}
	if len(params.ContentChanges) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	text := params.ContentChanges[len(params.ContentChanges)-1].Text
	s.documents[uri] = text
	return s.publishDiagnostics(uri, s.compileDiagnostics(uri, text))
}

func (s *flowLanguageServer) handleDidClose(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" {
		return s.invalidParams(message, errors.New("textDocument.uri is required"))
	}
	delete(s.documents, params.TextDocument.URI)
	return s.publishDiagnostics(params.TextDocument.URI, nil)
}

func (s *flowLanguageServer) handleCompletion(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
		Context      struct {
			TriggerKind      int    `json:"triggerKind"`
			TriggerCharacter string `json:"triggerCharacter"`
		} `json:"context"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and position are required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	return s.respond(message.ID, s.completions(*params.Position, params.Context.TriggerKind, params.Context.TriggerCharacter))
}

func (s *flowLanguageServer) handleHover(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
		Position     *lspPosition               `json:"position"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.Position == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and position are required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	return s.respond(message.ID, map[string]any{
		"contents": map[string]string{
			"kind":  "markdown",
			"value": "**Nexss Flow** node or DSL construct.",
		},
	})
}

func (s *flowLanguageServer) handleFormatting(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocumentIdentifier `json:"textDocument"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" {
		return s.invalidParams(message, errors.New("textDocument.uri is required"))
	}
	if len(message.ID) == 0 {
		return nil
	}
	uri := params.TextDocument.URI
	text := s.documents[uri]
	formatter, err := s.formatterFor(uri, text)
	if err != nil {
		return s.formattingError(message.ID, uri, text, err)
	}
	formatted, err := formatter.Format(s.ctx, text, s.sourceName(uri))
	if err != nil {
		return s.formattingError(message.ID, uri, text, err)
	}
	edit := map[string]any{
		"range":   lspRange{Start: lspPosition{}, End: lspEndPosition(text)},
		"newText": formatted,
	}
	return s.respond(message.ID, []any{edit})
}

func (s *flowLanguageServer) formattingError(id json.RawMessage, uri, text string, err error) error {
	err = lspRequirementError(err)
	if publishErr := s.publishDiagnostics(uri, []lspDiagnostic{diagnosticFromError(text, err)}); publishErr != nil {
		return publishErr
	}
	return s.respondError(id, -32603, err.Error())
}

func (s *flowLanguageServer) decodeParams(message lspMessage, target any) (bool, error) {
	if err := json.Unmarshal(message.Params, target); err != nil {
		return false, s.invalidParams(message, err)
	}
	return true, nil
}

func (s *flowLanguageServer) invalidParams(message lspMessage, err error) error {
	messageText := fmt.Sprintf("Invalid params for %s: %v", message.Method, err)
	if len(message.ID) != 0 {
		return s.respondError(message.ID, -32602, messageText)
	}
	slog.Warn("ignoring malformed LSP notification", "method", message.Method, "error", err)
	return nil
}

func (s *flowLanguageServer) compileDiagnostics(uri, source string) []lspDiagnostic {
	name := s.sourceName(uri)
	_, _, cfg, err := s.inv.prepareRequiredSource(s.ctx, source, name)
	if err != nil {
		return []lspDiagnostic{diagnosticFromError(source, lspRequirementError(err))}
	}
	if _, err := runner.Compile(s.ctx, cfg, source, name); err != nil {
		return []lspDiagnostic{diagnosticFromError(source, err)}
	}
	return nil
}

func (s *flowLanguageServer) formatterFor(uri, source string) (*flowformatter.Formatter, error) {
	_, metadata, cfg, err := s.inv.prepareRequiredSource(s.ctx, source, s.sourceName(uri))
	if err != nil {
		return nil, err
	}
	_, requireEnabled := cfg.Directives.ByName("require")
	return flowformatter.NewWithParserTables(cfg.Operators, cfg.PrimariesFor(metadata), requireEnabled), nil
}

func lspRequirementError(err error) error {
	if strings.Contains(err.Error(), "is not registered in this binary") {
		return fmt.Errorf("%w; nflow lsp only supports bundles linked into this binary; save the file and run `nflow lint <file>` for full @require validation", err)
	}
	return err
}

func (s *flowLanguageServer) completions(position lspPosition, triggerKind int, triggerCharacter string) map[string]any {
	items := make([]map[string]any, 0, len(flowSnippets))
	for _, snippet := range flowSnippets {
		start := position
		if triggerKind == 2 && triggerCharacter == "@" && strings.HasPrefix(snippet.insertText, "@") && start.Character > 0 {
			start.Character--
		}
		items = append(items, map[string]any{
			"label":            snippet.label,
			"kind":             15,
			"detail":           snippet.detail,
			"insertText":       snippet.insertText,
			"insertTextFormat": 2,
			"textEdit": map[string]any{
				"newText": snippet.insertText,
				"range":   lspRange{Start: start, End: position},
			},
		})
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

func (s *flowLanguageServer) publishDiagnostics(uri string, diagnostics []lspDiagnostic) error {
	return s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": diagnostics,
	})
}

func (s *flowLanguageServer) respond(id json.RawMessage, result any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *flowLanguageServer) respondError(id json.RawMessage, code int, message string) error {
	return s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

func (s *flowLanguageServer) notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *flowLanguageServer) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if _, writeErr := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(body)); writeErr != nil {
		return writeErr
	}
	_, err = s.out.Write(body)
	return err
}

func readLSPFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			return nil, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid LSP Content-Length %q", value)
			}
			contentLength = parsed
		}
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing LSP Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func diagnosticFromError(source string, err error) lspDiagnostic {
	start := lspPosition{}
	if position, ok := compiler.PositionOf(err); ok {
		start = compilerPositionToLSP(source, position)
	}
	end := start
	if start.Character < lspLineLength(source, start.Line) {
		end.Character++
	}
	return lspDiagnostic{
		Range:    lspRange{Start: start, End: end},
		Severity: 1,
		Source:   lspSource,
		Message:  err.Error(),
	}
}

func compilerPositionToLSP(source string, position compiler.Position) lspPosition {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	line := max(position.Line-1, 0)
	if line >= len(lines) {
		line = len(lines) - 1
	}
	if line < 0 {
		line = 0
	}
	text := strings.TrimSuffix(lines[line], "\r")
	column := max(position.Col-1, 0)
	column = min(column, len(text))
	for column > 0 && column < len(text) && !utf8.RuneStart(text[column]) {
		column--
	}
	return lspPosition{Line: line, Character: utf16Length(text[:column])}
}

func lspLineLength(source string, line int) int {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if line < 0 || line >= len(lines) {
		return 0
	}
	return utf16Length(strings.TrimSuffix(lines[line], "\r"))
}

func utf16Length(text string) int {
	return len(utf16.Encode([]rune(text)))
}

func lspEndPosition(source string) lspPosition {
	line := strings.Count(source, "\n")
	lastLine := source
	if index := strings.LastIndexByte(source, '\n'); index >= 0 {
		lastLine = source[index+1:]
	}
	lastLine = strings.TrimSuffix(lastLine, "\r")
	return lspPosition{Line: line, Character: utf16Length(lastLine)}
}

func lspSourceName(uri string) string {
	if name := lspFileURIPath(uri); name != "" {
		return name
	}
	return uri
}

func (s *flowLanguageServer) sourceName(uri string) string {
	parsed, err := url.Parse(uri)
	if err == nil && parsed.Scheme == "untitled" && s.rootPath != "" {
		uriPath := parsed.Path
		if uriPath == "" {
			uriPath = parsed.Opaque
		}
		base := filepath.Base(uriPath)
		if base == "." || base == "/" || base == "" {
			base = "untitled"
		}
		if filepath.Ext(base) == "" {
			base += ".nflow"
		}
		return filepath.Join(s.rootPath, base)
	}
	return lspSourceName(uri)
}

func lspFileURIPath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	name := parsed.Path
	if len(name) >= 3 && name[0] == '/' && name[2] == ':' {
		name = name[1:]
	}
	return filepath.FromSlash(name)
}
