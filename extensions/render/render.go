package render

import (
	"bytes"
	"iter"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/extensions/fs"
)

// Config controls render.markdown.
type Config struct {
	// Editor selects the framing: "markdown" (default), "zed", "claude",
	// or "fold" (details/summary).
	Editor   string `json:"editor"    cli:"editor,e"`
	NoHeader bool   `json:"no_header" cli:"no_header"`
	Prompt   string `json:"prompt"    cli:"prompt"`
}

// renderBufPool amortizes the formatting buffer across items.
var renderBufPool = sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, 16*1024)) },
}

// Markdown concatenates every file into a single Markdown stream.
// Each file's Content is replaced with its formatted representation;
// FileMeta is preserved so downstream stages can still inspect paths.
func Markdown(cfg Config) action.StreamOp[fs.FileContent, fs.FileContent] {
	editor := strings.ToLower(strings.TrimSpace(cfg.Editor))
	if editor == "" {
		editor = "markdown"
	}

	return func(up iter.Seq2[fs.FileContent, error]) iter.Seq2[fs.FileContent, error] {
		return func(yield func(fs.FileContent, error) bool) {
			buffer, ok := renderBufPool.Get().(*bytes.Buffer)
			if !ok {
				// Unreachable: renderBufPool.New always returns
				// *bytes.Buffer. Defensive guard so a future pool
				// change cannot panic here.
				buffer = bytes.NewBuffer(make([]byte, 0, 16*1024))
			}
			defer renderBufPool.Put(buffer)

			if cfg.Prompt != "" && !cfg.NoHeader {
				buffer.WriteString("> ")
				buffer.WriteString(strings.ReplaceAll(strings.TrimSpace(cfg.Prompt), "\n", "\n> "))
				buffer.WriteString("\n\n")
			}

			for file, err := range up {
				if err != nil {
					yield(fs.FileContent{}, err)
					return
				}

				buffer.Reset()
				lang := file.Lang
				if lang == "" {
					lang = "text"
				}
				relPath := file.RelPath
				if relPath == "" {
					relPath = file.Path
				}

				formatFileBlock(buffer, editor, relPath, lang, file.Content, cfg.NoHeader)

				item := fs.FileContent{
					FileMeta: file.FileMeta,
					Content:  bytes.Clone(buffer.Bytes()),
				}
				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func MarkdownOperator() action.NamedOperator {
	return action.NewOperator("render.markdown", Markdown)
}

// formatFileBlock writes one framed file block into buffer. The
// editor argument is already lowercased and defaults to "markdown".
func formatFileBlock(buffer *bytes.Buffer, editor, relPath, lang string, content []byte, noHeader bool) {
	switch editor {
	case "zed":
		writeFencedBlock(buffer, lang, relPath, content)

	case "claude":
		buffer.WriteString("<file path=\"")
		buffer.WriteString(relPath)
		buffer.WriteString("\" language=\"")
		buffer.WriteString(lang)
		buffer.WriteString("\">\n")
		writeFencedBlock(buffer, lang, "", content)
		buffer.WriteString("</file>\n\n")

	case "fold":
		buffer.WriteString("<details>\n<summary>")
		buffer.WriteString(relPath)
		buffer.WriteString("</summary>\n\n")
		writeFencedBlock(buffer, lang, "", content)
		buffer.WriteString("</details>\n\n")

	default: // markdown
		if !noHeader {
			buffer.WriteString("### ")
			buffer.WriteString(relPath)
			buffer.WriteString(" (")
			buffer.WriteString(lang)
			buffer.WriteString(")\n")
		}
		writeFencedBlock(buffer, lang, "", content)
	}
}

// writeFencedBlock emits a triple-backtick fenced code block. A
// non-empty info string after the language is appended on the opening
// fence line (used by the "zed" editor to carry the path).
func writeFencedBlock(buffer *bytes.Buffer, lang, info string, content []byte) {
	buffer.WriteString("```")
	buffer.WriteString(lang)
	if info != "" {
		buffer.WriteByte(' ')
		buffer.WriteString(info)
	}
	buffer.WriteByte('\n')
	buffer.Write(content)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		buffer.WriteByte('\n')
	}
	buffer.WriteString("```\n\n")
}
