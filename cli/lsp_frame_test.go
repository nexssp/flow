package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestLSPFrameRejectsMalformedAndTruncatedFrames(t *testing.T) {
	cases := []struct {
		name              string
		data              string
		wantUnexpectedEOF bool
	}{
		{name: "missing content length", data: "X-Test: 1\r\n\r\n{}"},
		{name: "duplicate content length", data: "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}"},
		{name: "negative content length", data: "Content-Length: -1\r\n\r\n"},
		{name: "signed content length", data: "Content-Length: +2\r\n\r\n{}"},
		{name: "non-numeric content length", data: "Content-Length: x\r\n\r\n{}"},
		{name: "oversized body", data: fmt.Sprintf("Content-Length: %d\r\n\r\n", maxLSPMessageBytes+1)},
		{name: "LF-only headers", data: "Content-Length: 2\n\n{}"},
		{name: "truncated header after complete line", data: "Content-Length: 2\r\n", wantUnexpectedEOF: true},
		{name: "truncated header line", data: "Content-Length: 2", wantUnexpectedEOF: true},
		{name: "truncated body with no bytes", data: "Content-Length: 2\r\n\r\n", wantUnexpectedEOF: true},
		{name: "truncated body", data: "Content-Length: 2\r\n\r\n{", wantUnexpectedEOF: true},
		{name: "oversized header", data: "X-Test: " + strings.Repeat("a", maxLSPHeaderBytes) + "\r\n\r\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := readLSPFrame(bufio.NewReader(strings.NewReader(test.data)))
			if err == nil {
				t.Fatal("readLSPFrame succeeded; want an error")
			}
			if test.wantUnexpectedEOF && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("frame error = %v, want io.ErrUnexpectedEOF", err)
			}
		})
	}
	if _, err := readLSPFrame(bufio.NewReader(strings.NewReader(""))); !errors.Is(err, io.EOF) {
		t.Fatalf("empty stream error = %v, want io.EOF", err)
	}
}

func TestLSPFrameReadAndWriteHandleShortWrites(t *testing.T) {
	writer := &shortLSPFrameWriter{limit: 1}
	if err := writeLSPAll(writer, []byte("Content-Length: 2\r\n\r\n{}")); err != nil {
		t.Fatal(err)
	}
	body, err := readLSPFrame(bufio.NewReader(bytes.NewReader(writer.Bytes())))
	if err != nil || string(body) != "{}" {
		t.Fatalf("short-write frame body=%q err=%v", body, err)
	}
	if err := writeLSPAll(&shortLSPFrameWriter{}, []byte("x")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("zero-progress write error = %v, want io.ErrShortWrite", err)
	}
}

type shortLSPFrameWriter struct {
	bytes.Buffer
	limit int
}

func (writer *shortLSPFrameWriter) Write(data []byte) (int, error) {
	if writer.limit == 0 {
		return 0, nil
	}
	if len(data) > writer.limit {
		data = data[:writer.limit]
	}
	return writer.Buffer.Write(data)
}

func TestLSPEnvelopePreservesNullRequestID(t *testing.T) {
	message, err := decodeLSPMessage([]byte(`{"jsonrpc":"2.0","id":null,"method":"shutdown"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(message.ID) != "null" || message.Method != "shutdown" {
		t.Fatalf("decoded null-ID request = %#v, want id null and shutdown method", message)
	}
	if _, err := decodeLSPMessage([]byte(`{"jsonrpc":"2.0","id":true,"method":"shutdown"}`)); !errors.Is(err, errInvalidLSPRequest) {
		t.Fatalf("boolean request ID error = %v, want invalid JSON-RPC request", err)
	}
}

func TestLSPDiagnosticRangeUsesWholeUTF16CodePoint(t *testing.T) {
	start, end := compilerPositionRangeToLSP("😀x", compiler.Position{Line: 1, Col: 1})
	if start != (lspPosition{Line: 0, Character: 0}) || end != (lspPosition{Line: 0, Character: 2}) {
		t.Fatalf("UTF-16 range = %#v..%#v, want 0..2 for the astral rune", start, end)
	}
}
