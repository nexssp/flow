package core

import (
	"context"
	"strings"
	"testing"
)

func TestParserErrorIncludesFilePath(t *testing.T) {
	t.Parallel()

	_, err := NewParserWithFile(context.Background(), NewOperatorTable(), nil,
		`const(value=x) giberish`, "flows/test.nflow").Parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.HasPrefix(err.Error(), "flows/test.nflow:1:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "flows/test.nflow:1:")
	}
}

func TestParserErrorWithoutFileStartsWithLine(t *testing.T) {
	t.Parallel()

	_, err := NewParser(context.Background(), NewOperatorTable(), `((((`).Parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.HasPrefix(err.Error(), "<input>:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "<input>:")
	}
}
