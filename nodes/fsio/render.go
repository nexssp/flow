package fsio

import (
	"bytes"
	"iter"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
)

type RenderConfig struct {
	Editor   string `json:"editor" cli:"editor,e"`
	NoHeader bool   `json:"no_header" cli:"no_header"`
	Prompt   string `json:"prompt" cli:"prompt"`
}

var renderBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 16*1024))
	},
}

func RenderMarkdown(cfg RenderConfig) action.StreamOp[FileContent, FileContent] {
	editor := strings.ToLower(strings.TrimSpace(cfg.Editor))
	if editor == "" {
		editor = "markdown"
	}

	return func(up iter.Seq2[FileContent, error]) iter.Seq2[FileContent, error] {
		return func(yield func(FileContent, error) bool) {
			buf := renderBufPool.Get().(*bytes.Buffer)
			defer renderBufPool.Put(buf)

			if cfg.Prompt != "" && !cfg.NoHeader {
				buf.WriteString("> ")
				buf.WriteString(strings.ReplaceAll(strings.TrimSpace(cfg.Prompt), "\n", "\n> "))
				buf.WriteString("\n\n")
			}

			for file, err := range up {
				if err != nil {
					var zero FileContent
					yield(zero, err)
					return
				}

				buf.Reset()

				lang := file.Lang
				if lang == "" {
					lang = "text"
				}

				relPath := file.RelPath
				if relPath == "" {
					relPath = file.Path
				}

				formatFileBlock(buf, editor, relPath, lang, file.Content, cfg.NoHeader)

				item := FileContent{
					FileMeta: file.FileMeta,
					Content:  bytes.Clone(buf.Bytes()),
				}

				if !yield(item, nil) {
					return
				}
			}
		}
	}
}

func formatFileBlock(buf *bytes.Buffer, editor, relPath, lang string, content []byte, noHeader bool) {
	switch editor {
	case "zed":
		buf.WriteString("```")
		buf.WriteString(lang)
		buf.WriteByte(' ')
		buf.WriteString(relPath)
		buf.WriteByte('\n')
		buf.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n\n")

	case "claude":
		buf.WriteString("<file path=\"")
		buf.WriteString(relPath)
		buf.WriteString("\" language=\"")
		buf.WriteString(lang)
		buf.WriteString("\">\n```")
		buf.WriteString(lang)
		buf.WriteByte('\n')
		buf.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n</file>\n\n")

	case "fold":
		buf.WriteString("<details>\n<summary>")
		buf.WriteString(relPath)
		buf.WriteString("</summary>\n\n```")
		buf.WriteString(lang)
		buf.WriteByte('\n')
		buf.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n</details>\n\n")

	default: // "markdown"
		if !noHeader {
			buf.WriteString("### ")
			buf.WriteString(relPath)
			buf.WriteString(" (")
			buf.WriteString(lang)
			buf.WriteString(")\n")
		}
		buf.WriteString("```")
		buf.WriteString(lang)
		buf.WriteByte('\n')
		buf.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
		buf.WriteString("```\n\n")
	}
}

func RenderMarkdownOperator() action.NamedOperator {
	return action.NewOperator("render.markdown", RenderMarkdown)
}
