package fsio_test

import (
	"strings"
	"testing"

	"github.com/nexssp/flow/nodes/fsio"
	"github.com/nexssp/kernel/action"
)

func TestRenderMarkdown_Standard(t *testing.T) {
	t.Parallel()

	input := []fsio.FileContent{
		{
			FileMeta: fsio.FileMeta{
				RelPath: "service/auth.go",
				Lang:    "go",
			},
			Content: []byte("func Login() bool { return true }"),
		},
	}

	stream := action.StreamFromSlice(input)
	renderOp := fsio.RenderMarkdown(fsio.RenderConfig{Editor: "markdown"})

	var result []fsio.FileContent
	for item, err := range renderOp(stream) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result = append(result, item)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	output := string(result[0].Content)
	if !strings.Contains(output, "### service/auth.go (go)") {
		t.Errorf("missing header in markdown output:\n%s", output)
	}
	if !strings.Contains(output, "```go\nfunc Login() bool { return true }\n```") {
		t.Errorf("missing code block in markdown output:\n%s", output)
	}
}

func TestRenderMarkdown_Zed(t *testing.T) {
	t.Parallel()

	input := []fsio.FileContent{
		{
			FileMeta: fsio.FileMeta{
				RelPath: "main.py",
				Lang:    "python",
			},
			Content: []byte("print('hello')"),
		},
	}

	stream := action.StreamFromSlice(input)
	renderOp := fsio.RenderMarkdown(fsio.RenderConfig{Editor: "zed"})

	var result []fsio.FileContent
	for item, err := range renderOp(stream) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result = append(result, item)
	}

	output := string(result[0].Content)
	expected := "```python main.py\nprint('hello')\n```\n\n"
	if output != expected {
		t.Errorf("got %q, want %q", output, expected)
	}
}
