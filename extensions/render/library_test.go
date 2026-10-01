package render

import (
	"bytes"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/extensions/fs"
)

func TestBundle_WiresLibrary(t *testing.T) {
	t.Parallel()
	b := Bundle(nil)
	ktest.RequireEqual(t, b.ID, ID)
	ktest.RequireEqual(t, len(b.Libraries), 1)
	ktest.RequireEqual(t, b.Libraries[0].Name, ID)
	ktest.RequireCondition(t, b.SelfTest != nil, "SelfTest is nil")
}

func TestLibrary_HasMarkdownOperator(t *testing.T) {
	t.Parallel()
	lib := Library()
	ktest.RequireEqual(t, len(lib.Operators), 1)
	ktest.RequireEqual(t, lib.Operators[0].Name, "render.markdown")
}

func TestFormatFileBlock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		editor   string
		expected string
	}{
		{
			name:     "markdown with header",
			editor:   "markdown",
			expected: "### a.go (go)\n```go\npackage a\n```\n\n",
		},
		{
			name:     "zed",
			editor:   "zed",
			expected: "```go a.go\npackage a\n```\n\n",
		},
		{
			name:     "claude",
			editor:   "claude",
			expected: "<file path=\"a.go\" language=\"go\">\n```go\npackage a\n```\n\n</file>\n\n",
		},
		{
			name:     "fold",
			editor:   "fold",
			expected: "<details>\n<summary>a.go</summary>\n\n```go\npackage a\n```\n\n</details>\n\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			formatFileBlock(&buffer, c.editor, "a.go", "go", []byte("package a"), false)
			ktest.RequireEqual(t, buffer.String(), c.expected)
		})
	}
}

func TestMarkdown_IdentityOfMeta(t *testing.T) {
	t.Parallel()
	in := fs.FileContent{
		FileMeta: fs.FileMeta{Path: "a.go", RelPath: "a.go", Lang: "go"},
		Content:  []byte("package a"),
	}

	var result fs.FileContent
	for item, err := range Markdown(Config{})(singleSeq(in)) {
		ktest.RequireNoError(t, err)
		result = item
	}
	ktest.RequireEqual(t, result.RelPath, "a.go")
	ktest.RequireEqual(t, result.Lang, "go")
}

func singleSeq(item fs.FileContent) func(yield func(fs.FileContent, error) bool) {
	return func(yield func(fs.FileContent, error) bool) {
		yield(item, nil)
	}
}
