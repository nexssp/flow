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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/nexssp/flow/compiler"
	flowformatter "github.com/nexssp/flow/formatter"
	"github.com/nexssp/flow/runner"
)

const lspSource = "nflow"

const (
	maxLSPHeaderBytes  = 16 * 1024
	maxLSPMessageBytes = 16 * 1024 * 1024
)

type flowLanguageServer struct {
	ctx             context.Context
	inv             *invocation
	rootPath        string
	documents       map[string]string
	documentVersion map[string]int
	initialized     bool
	shutdown        bool
	exitCode        int
	out             io.Writer
	writeMu         sync.Mutex
}

type lspMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type lspTextDocument struct {
	URI     string  `json:"uri"`
	Text    *string `json:"text"`
	Version *int    `json:"version"`
}

type lspTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version"`
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
	return server.exitCode
}

func newFlowLanguageServer(ctx context.Context, inv *invocation) *flowLanguageServer {
	return &flowLanguageServer{
		ctx:             ctx,
		inv:             inv,
		documents:       make(map[string]string),
		documentVersion: make(map[string]int),
		exitCode:        1,
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
		message, decodeErr := decodeLSPMessage(body)
		if decodeErr != nil {
			if errors.Is(decodeErr, errInvalidLSPParams) {
				if len(message.ID) == 0 {
					slog.Warn("ignoring malformed LSP notification", "method", message.Method, "error", decodeErr)
					continue
				}
				if writeErr := s.respondError(message.ID, -32602, decodeErr.Error()); writeErr != nil {
					return writeErr
				}
				continue
			}
			code := -32700
			messageText := "parse error: " + decodeErr.Error()
			if errors.Is(decodeErr, errInvalidLSPRequest) {
				code = -32600
				messageText = decodeErr.Error()
			}
			if writeErr := s.respondError(json.RawMessage("null"), code, messageText); writeErr != nil {
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

var (
	errInvalidLSPRequest = errors.New("invalid JSON-RPC request")
	errInvalidLSPParams  = errors.New("invalid JSON-RPC params")
)

func decodeLSPMessage(body []byte) (lspMessage, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return lspMessage{}, err
	}
	if _, ok := value.(map[string]any); !ok {
		return lspMessage{}, fmt.Errorf("%w: message must be a JSON object", errInvalidLSPRequest)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return lspMessage{}, fmt.Errorf("%w: %w", errInvalidLSPRequest, err)
	}
	var message lspMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return lspMessage{}, fmt.Errorf("%w: %w", errInvalidLSPRequest, err)
	}
	if message.JSONRPC != "2.0" || message.Method == "" {
		return lspMessage{}, fmt.Errorf("%w: jsonrpc must be \"2.0\" and method must be a non-empty string", errInvalidLSPRequest)
	}
	if id, exists := fields["id"]; exists && !validLSPRequestID(id) {
		return lspMessage{}, fmt.Errorf("%w: id must be a string, number, or null", errInvalidLSPRequest)
	}
	if params, exists := fields["params"]; exists {
		trimmed := strings.TrimSpace(string(params))
		if trimmed == "" || trimmed[0] != '{' && trimmed[0] != '[' {
			return message, fmt.Errorf("%w: params must be an object or array", errInvalidLSPParams)
		}
	}
	return message, nil
}

func validLSPRequestID(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if value == "null" {
		return true
	}
	if value == "" {
		return false
	}
	if value[0] == '"' {
		var id string
		return json.Unmarshal(raw, &id) == nil
	}
	if value[0] == '-' || value[0] >= '0' && value[0] <= '9' {
		var id json.Number
		return json.Unmarshal(raw, &id) == nil
	}
	return false
}

func (s *flowLanguageServer) handle(message lspMessage) error {
	proceed, err := s.validateMethodKind(message)
	if !proceed || err != nil {
		return err
	}
	if message.Method == "exit" {
		if s.shutdown {
			s.exitCode = 0
		} else {
			s.exitCode = 1
		}
		return nil
	}
	proceed, err = s.validateLifecycle(message)
	if !proceed || err != nil {
		return err
	}
	return s.dispatch(message)
}

func (s *flowLanguageServer) validateMethodKind(message lspMessage) (bool, error) {
	switch message.Method {
	case "initialized", "$/cancelRequest", "exit", "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
		if len(message.ID) != 0 {
			return false, s.respondError(message.ID, -32600, message.Method+" is a notification and must not have an id")
		}
	case "initialize", "shutdown", "textDocument/completion", "textDocument/hover", "textDocument/formatting":
		if len(message.ID) == 0 {
			slog.Warn("ignoring LSP request method sent as a notification", "method", message.Method)
			return false, nil
		}
	}
	return true, nil
}

func (s *flowLanguageServer) validateLifecycle(message lspMessage) (bool, error) {
	if message.Method != "initialize" && message.Method != "$/cancelRequest" && !s.initialized {
		if len(message.ID) != 0 {
			return false, s.respondError(message.ID, -32002, "server not initialized")
		}
		slog.Warn("ignoring LSP notification before initialize", "method", message.Method)
		return false, nil
	}
	if s.shutdown {
		if len(message.ID) != 0 {
			return false, s.respondError(message.ID, -32600, "server has been shut down")
		}
		slog.Warn("ignoring LSP notification after shutdown", "method", message.Method)
		return false, nil
	}
	return true, nil
}

func (s *flowLanguageServer) dispatch(message lspMessage) error {
	switch message.Method {
	case "initialize":
		if s.initialized {
			if len(message.ID) != 0 {
				return s.respondError(message.ID, -32600, "initialize may only be requested once")
			}
			return nil
		}
		return s.handleInitialize(message)
	case "initialized":
		return nil
	case "$/cancelRequest":
		// All current handlers are synchronous; no request work remains in flight
		// while this notification is read, so there is nothing to cancel.
		return nil
	case "shutdown":
		if len(message.ID) == 0 {
			return s.invalidParams(message, errors.New("shutdown must be a request with an id"))
		}
		if err := s.respond(message.ID, nil); err != nil {
			return err
		}
		s.shutdown = true
		s.exitCode = 0
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
	err = s.respond(message.ID, map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync":           map[string]any{"openClose": true, "change": 1},
			"documentFormattingProvider": true,
			"completionProvider":         map[string]any{"triggerCharacters": []string{"@", ":", ".", " ", "-", "|", "&", "?", "{"}},
			"hoverProvider":              true,
		},
		"serverInfo": map[string]string{"name": "nflow", versionCommand: compiler.Version},
	})
	if err == nil {
		s.initialized = true
	}
	return err
}

func (s *flowLanguageServer) handleDidOpen(message lspMessage) error {
	var params struct {
		TextDocument *lspTextDocument `json:"textDocument"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.TextDocument.Text == nil || params.TextDocument.Version == nil {
		return s.invalidParams(message, errors.New("textDocument.uri, text, and version are required"))
	}
	uri := params.TextDocument.URI
	if _, exists := s.documents[uri]; exists {
		slog.Warn("ignoring duplicate LSP document open", "uri", uri)
		return nil
	}
	text, version := *params.TextDocument.Text, *params.TextDocument.Version
	s.documents[uri] = text
	s.documentVersion[uri] = version
	return s.publishDiagnostics(uri, s.compileDiagnostics(uri, text), &version)
}

func (s *flowLanguageServer) handleDidChange(message lspMessage) error {
	var params struct {
		TextDocument   *lspTextDocumentIdentifier `json:"textDocument"`
		ContentChanges []struct {
			Text        *string   `json:"text"`
			Range       *lspRange `json:"range"`
			RangeLength *int      `json:"rangeLength"`
		} `json:"contentChanges"`
	}
	valid, err := s.decodeParams(message, &params)
	if err != nil || !valid {
		return err
	}
	if params.TextDocument == nil || params.TextDocument.URI == "" || params.TextDocument.Version == nil {
		return s.invalidParams(message, errors.New("textDocument.uri and version are required"))
	}
	if len(params.ContentChanges) == 0 {
		return s.invalidParams(message, errors.New("contentChanges must contain a full-document text update"))
	}
	uri := params.TextDocument.URI
	if _, exists := s.documents[uri]; !exists {
		slog.Warn("ignoring change for unopened LSP document", "uri", uri)
		return nil
	}
	for i, change := range params.ContentChanges {
		if change.Text == nil || change.Range != nil || change.RangeLength != nil {
			return s.invalidParams(message, fmt.Errorf("contentChanges[%d] must be a full-document text update", i))
		}
	}
	version := *params.TextDocument.Version
	if version <= s.documentVersion[uri] {
		slog.Debug("ignoring stale LSP document change", "uri", uri, "version", version, "currentVersion", s.documentVersion[uri])
		return nil
	}
	text := *params.ContentChanges[len(params.ContentChanges)-1].Text
	s.documents[uri] = text
	s.documentVersion[uri] = version
	return s.publishDiagnostics(uri, s.compileDiagnostics(uri, text), &version)
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
	uri := params.TextDocument.URI
	version, hadDocument := s.documentVersion[uri]
	delete(s.documents, uri)
	delete(s.documentVersion, uri)
	var versionPtr *int
	if hadDocument {
		versionPtr = &version
	}
	return s.publishDiagnostics(uri, []lspDiagnostic{}, versionPtr)
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
	text, exists := s.documents[uri]
	if !exists {
		return s.invalidParams(message, fmt.Errorf("document %q is not open", uri))
	}
	version := s.documentVersion[uri]
	formatter, err := s.formatterFor(uri, text)
	if err != nil {
		return s.formattingError(message.ID, uri, text, &version, err)
	}
	formatted, err := formatter.Format(s.ctx, text, s.sourceName(uri))
	if err != nil {
		return s.formattingError(message.ID, uri, text, &version, err)
	}
	edit := map[string]any{
		"range":   lspRange{Start: lspPosition{}, End: lspEndPosition(text)},
		"newText": formatted,
	}
	return s.respond(message.ID, []any{edit})
}

func (s *flowLanguageServer) formattingError(id json.RawMessage, uri, text string, version *int, err error) error {
	err = lspRequirementError(err)
	if publishErr := s.publishDiagnostics(uri, []lspDiagnostic{diagnosticFromError(text, err)}, version); publishErr != nil {
		return publishErr
	}
	return s.respondError(id, -32603, err.Error())
}

func (s *flowLanguageServer) decodeParams(message lspMessage, target any) (bool, error) {
	params := strings.TrimSpace(string(message.Params))
	if params == "" || params == "null" {
		return false, s.invalidParams(message, errors.New("params object is required"))
	}
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
	return flowformatter.NewWithParserTables(cfg.Operators, cfg.PrimariesFor(metadata), requireEnabled, cfg.Directives.All()...), nil
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

func (s *flowLanguageServer) publishDiagnostics(uri string, diagnostics []lspDiagnostic, version *int) error {
	if diagnostics == nil {
		diagnostics = []lspDiagnostic{}
	}
	params := map[string]any{"uri": uri, "diagnostics": diagnostics}
	if version != nil {
		params["version"] = *version
	}
	return s.notify("textDocument/publishDiagnostics", params)
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
	if len(body) > maxLSPMessageBytes {
		return fmt.Errorf("outgoing LSP message exceeds %d-byte limit", maxLSPMessageBytes)
	}
	if s.out == nil {
		return errors.New("nflow lsp: output is unavailable")
	}
	frame := make([]byte, 0, len(body)+32)
	frame = append(frame, []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))...)
	frame = append(frame, body...)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeLSPAll(s.out, frame)
}

func readLSPFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength, err := readLSPHeader(reader)
	if err != nil {
		return nil, err
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return body, nil
}

func readLSPHeader(reader *bufio.Reader) (int, error) {
	contentLength := -1
	headerBytes := 0
	for {
		line, err := readLSPHeaderLine(reader, maxLSPHeaderBytes-headerBytes)
		if errors.Is(err, io.EOF) {
			if headerBytes == 0 {
				return 0, io.EOF
			}
			return 0, io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, err
		}
		headerBytes += len(line)
		if !strings.HasSuffix(string(line), "\r\n") {
			return 0, errors.New("LSP headers must use CRLF line endings")
		}
		line = line[:len(line)-2]
		if len(line) == 0 {
			break
		}
		if err := parseLSPHeaderLine(string(line), &contentLength); err != nil {
			return 0, err
		}
	}
	if contentLength < 0 {
		return 0, errors.New("missing LSP Content-Length header")
	}
	return contentLength, nil
}

func parseLSPHeaderLine(line string, contentLength *int) error {
	name, value, ok := strings.Cut(line, ":")
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("invalid LSP header %q", line)
	}
	if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
		return nil
	}
	if *contentLength >= 0 {
		return errors.New("duplicate LSP Content-Length header")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("invalid empty LSP Content-Length")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("invalid LSP Content-Length %q", value)
		}
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fmt.Errorf("invalid LSP Content-Length %q", value)
	}
	if parsed > maxLSPMessageBytes {
		return fmt.Errorf("LSP message exceeds %d-byte limit", maxLSPMessageBytes)
	}
	*contentLength = parsed
	return nil
}

func readLSPHeaderLine(reader *bufio.Reader, remaining int) ([]byte, error) {
	if remaining <= 0 {
		return nil, fmt.Errorf("LSP headers exceed %d-byte limit", maxLSPHeaderBytes)
	}
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > remaining {
			return nil, fmt.Errorf("LSP headers exceed %d-byte limit", maxLSPHeaderBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

func writeLSPAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return fmt.Errorf("invalid LSP writer count %d", written)
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func diagnosticFromError(source string, err error) lspDiagnostic {
	start := lspPosition{}
	end := start
	if position, ok := compiler.PositionOf(err); ok {
		start, end = compilerPositionRangeToLSP(source, position)
	}
	return lspDiagnostic{
		Range:    lspRange{Start: start, End: end},
		Severity: 1,
		Source:   lspSource,
		Message:  err.Error(),
	}
}

func compilerPositionRangeToLSP(source string, position compiler.Position) (start, end lspPosition) {
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
	start = lspPosition{Line: line, Character: utf16Length(text[:column])}
	end = start
	if column < len(text) {
		_, size := utf8.DecodeRuneInString(text[column:])
		if size < 1 {
			size = 1
		}
		end.Character += utf16Length(text[column:min(column+size, len(text))])
	}
	return start, end
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
	if err != nil || !lspValidFileURI(parsed) {
		return ""
	}
	name := parsed.Path
	if name == "" {
		return ""
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return lspFileURIHostPath(parsed.Host, name)
	}
	if lspIsWindowsDrivePath(name) {
		if runtime.GOOS != windowsOS {
			return ""
		}
		name = name[1:]
	} else if !filepath.IsAbs(filepath.FromSlash(name)) {
		return ""
	}
	return filepath.FromSlash(name)
}

func lspValidFileURI(parsed *url.URL) bool {
	return parsed.Scheme == "file" && parsed.Opaque == "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func lspFileURIHostPath(host, name string) string {
	if runtime.GOOS != windowsOS {
		return ""
	}
	if lspIsWindowsDrive(host) {
		return filepath.FromSlash(host + "/" + strings.TrimLeft(name, "/"))
	}
	return filepath.Join(`\\`+host, filepath.FromSlash(name))
}

func lspIsWindowsDrive(path string) bool {
	return len(path) == 2 && path[1] == ':' &&
		((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z'))
}

func lspIsWindowsDrivePath(path string) bool {
	return len(path) >= 3 && path[0] == '/' && path[2] == ':'
}
