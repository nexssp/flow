package render_test

import (
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/extensions/fs"
	"github.com/nexssp/flow/extensions/render"
)

func TestRenderMarkdown_Standard(t *testing.T) {
	t.Parallel()

	input := []fs.FileContent{
		{
			RelPath: "service/auth.go",
			Lang:    "go",
			Content: []byte("func Login() bool { return true }"),
		},
	}

	stream := action.StreamFromSlice(input)
	renderOp := render.Markdown(render.Config{Editor: "markdown"})

	var result []fs.FileContent
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

func TestRenderMarkdown_Prompt(t *testing.T) {
	t.Parallel()
	input := []fs.FileContent{{RelPath: "one.txt", Lang: "text", Content: []byte("one")}, {RelPath: "two.txt", Lang: "text", Content: []byte("two")}}
	renderOp := render.Markdown(render.Config{Prompt: "Do this\ncarefully"})
	var result []fs.FileContent
	for item, err := range renderOp(action.StreamFromSlice(input)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result = append(result, item)
	}
	if len(result) != len(input) {
		t.Fatalf("expected %d results, got %d", len(input), len(result))
	}
	for i, item := range result {
		if !strings.HasPrefix(string(item.Content), "> Do this\n> carefully\n\n") {
			t.Errorf("result %d missing quoted prompt:\n%s", i, item.Content)
		}
	}
	renderOp = render.Markdown(render.Config{Prompt: "Do this", NoHeader: true})
	for item, err := range renderOp(action.StreamFromSlice(input[:1])) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		output := string(item.Content)
		if strings.Contains(output, "Do this") {
			t.Errorf("NoHeader output should suppress prompt:\n%s", output)
		}
		if strings.Contains(output, "### ") {
			t.Errorf("NoHeader output should suppress file header:\n%s", output)
		}
	}
}

func TestRenderMarkdown_Zed(t *testing.T) {
	t.Parallel()

	input := []fs.FileContent{
		{
			RelPath: "main.py",
			Lang:    "python",
			Content: []byte("print('hello')"),
		},
	}

	stream := action.StreamFromSlice(input)
	renderOp := render.Markdown(render.Config{Editor: "zed"})

	var result []fs.FileContent
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
