package pipeline

import (
	"errors"
	"fmt"
	"strings"

	compiler "github.com/nexssp/flow/compiler"
)

const pipelineSignaturesMetaKey = "pipeline_signatures"

type pipelineSignature struct {
	input  *schemaSelection
	output *schemaSelection
}

// parsePipelineDeclarationHeader extracts the optional signature and delegates
// all legacy modifier syntax and continuation handling to the compiler's shared
// declaration-header parser. Signature text is replaced with spaces of equal
// length so the parser's existing source offsets remain valid.
func parsePipelineDeclarationHeader(req compiler.DirectiveReq) (compiler.DeclarationHeader, pipelineSignature, bool, error) {
	line := req.Lines[req.I]
	nameEnd, hasSignature := pipelineSignatureNameEnd(line)
	if !hasSignature {
		header, err := compiler.ParseDeclarationHeaderWithSpans(req, ID)
		return header, pipelineSignature{}, false, err
	}

	signature, signatureEnd, err := parsePipelineSignature(req, line, nameEnd)
	if err != nil {
		return compiler.DeclarationHeader{}, pipelineSignature{}, true, err
	}

	lines := append([]string(nil), req.Lines...)
	lines[req.I] = line[:nameEnd] + strings.Repeat(" ", signatureEnd-nameEnd) + line[signatureEnd:]
	parsedReq := req
	parsedReq.Lines = lines
	header, err := compiler.ParseDeclarationHeaderWithSpans(parsedReq, ID)
	if err != nil {
		return compiler.DeclarationHeader{}, pipelineSignature{}, true, err
	}
	lastHeaderLine := max(header.Next-1, req.I)
	header.HeaderSpan.End = req.Offset(lastHeaderLine, len(req.Lines[lastHeaderLine]))
	return header, signature, true, nil
}

func pipelineSignatureNameEnd(line string) (int, bool) {
	keywordStart := strings.Index(line, "@pipeline")
	if keywordStart < 0 {
		return 0, false
	}
	nameEnd := keywordStart + len("@pipeline")
	for nameEnd < len(line) && (line[nameEnd] == ' ' || line[nameEnd] == '\t') {
		nameEnd++
	}
	for nameEnd < len(line) && !strings.ContainsRune("( :\t#", rune(line[nameEnd])) {
		nameEnd++
	}
	return nameEnd, nameEnd < len(line) && line[nameEnd] == '('
}

func parsePipelineSignature(req compiler.DirectiveReq, line string, nameEnd int) (pipelineSignature, int, error) {
	closeOffset := strings.IndexByte(line[nameEnd+1:], ')')
	if closeOffset < 0 {
		return pipelineSignature{}, 0, pipelineSignatureError(req, nameEnd, "unterminated pipeline signature; expected ')' after req parameter")
	}
	closeParen := nameEnd + 1 + closeOffset
	input, hasInput, err := parsePipelineInput(req, line[nameEnd+1:closeParen], nameEnd+1)
	if err != nil {
		return pipelineSignature{}, 0, err
	}

	signature := pipelineSignature{}
	if hasInput {
		signature.input = &input
	}
	signatureEnd := closeParen + 1
	output, outputEnd, err := parsePipelineOutput(req, line, signatureEnd)
	if err != nil {
		return pipelineSignature{}, 0, err
	}
	if output != nil {
		signature.output = output
		signatureEnd = outputEnd
	}
	return signature, signatureEnd, nil
}

func parsePipelineInput(req compiler.DirectiveReq, raw string, column int) (schemaSelection, bool, error) {
	parameterText := strings.TrimSpace(raw)
	if parameterText == "" {
		return schemaSelection{}, false, nil
	}
	parameterColon := strings.IndexByte(parameterText, ':')
	if parameterColon < 0 || strings.Contains(parameterText, ",") || strings.Count(parameterText, ":") != 1 {
		return schemaSelection{}, false, pipelineSignatureError(req, column, "signature accepts one named parameter in the form req: Schema")
	}
	if strings.TrimSpace(parameterText[:parameterColon]) != "req" {
		return schemaSelection{}, false, pipelineSignatureError(req, column, "the pipeline input parameter must be named req")
	}
	selection, err := parseSignatureSelection(parameterText[parameterColon+1:], "request-schema")
	if err != nil {
		return schemaSelection{}, false, pipelineSignatureError(req, column+parameterColon+1, "invalid req type: "+err.Error())
	}
	return selection, true, nil
}

func parsePipelineOutput(req compiler.DirectiveReq, line string, start int) (*schemaSelection, int, error) {
	returnColon := start
	for returnColon < len(line) && (line[returnColon] == ' ' || line[returnColon] == '\t') {
		returnColon++
	}
	if returnColon >= len(line) || line[returnColon] != ':' {
		return nil, start, nil
	}

	typeStart := returnColon + 1
	hasWhitespaceAfterColon := typeStart < len(line) && (line[typeStart] == ' ' || line[typeStart] == '\t')
	for typeStart < len(line) && (line[typeStart] == ' ' || line[typeStart] == '\t') {
		typeStart++
	}
	isPrefixedType := typeStart < len(line) && (line[typeStart] == '!' || strings.HasPrefix(line[typeStart:], "[]"))
	if !hasWhitespaceAfterColon && !isPrefixedType {
		return nil, start, nil
	}

	typeEnd := typeStart
	for typeEnd < len(line) && !strings.ContainsRune(" \t:#", rune(line[typeEnd])) {
		typeEnd++
	}
	if typeStart == typeEnd {
		return nil, 0, pipelineSignatureError(req, returnColon, "expected an output schema type after ':'")
	}
	selection, err := parseSignatureSelection(line[typeStart:typeEnd], "response-schema")
	if err != nil {
		return nil, 0, pipelineSignatureError(req, typeStart, "invalid output type: "+err.Error())
	}
	return &selection, typeEnd, nil
}

func pipelineSignatureError(req compiler.DirectiveReq, column int, message string) error {
	return compiler.SourceError(compiler.Position{File: req.File, Line: req.I + 1, Col: column + 1}, "@pipeline: %s", message)
}

func parseSignatureSelection(raw, modifierName string) (schemaSelection, error) {
	selected := strings.TrimSpace(raw)
	if selected == "" {
		return schemaSelection{}, errors.New("schema type is required")
	}
	strict := strings.HasPrefix(selected, "!")
	if strict {
		selected = strings.TrimPrefix(selected, "!")
	}
	array := strings.HasPrefix(selected, "[]")
	if array {
		selected = strings.TrimPrefix(selected, "[]")
	}
	if selected == "" || strings.HasPrefix(selected, "!") || strings.HasPrefix(selected, "[]") {
		return schemaSelection{}, errors.New("expected one schema name, optionally prefixed by ! or []")
	}
	if !validSignatureSchemaName(selected) {
		return schemaSelection{}, fmt.Errorf("invalid schema name %q", selected)
	}
	return schemaSelection{name: selected, strict: strict, array: array, modifierName: modifierName}, nil
}

func validSignatureSchemaName(selected string) bool {
	for segmentIndex, segment := range strings.Split(selected, ".") {
		if segment == "" {
			return false
		}
		for byteIndex := range len(segment) {
			c := segment[byteIndex]
			if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || byteIndex > 0 && c >= '0' && c <= '9' {
				continue
			}
			return false
		}
		if segmentIndex == 0 && segment[0] >= '0' && segment[0] <= '9' {
			return false
		}
	}
	return true
}
