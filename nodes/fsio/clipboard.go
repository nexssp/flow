package fsio

import (
	"bytes"
	"iter"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/nexssp/kernel/action"
)

type ClipboardConfig struct {
	Enabled bool `json:"enabled"`
}

func ClipboardWrite[T any](cfg ClipboardConfig) action.StreamOp[T, T] {
	return func(up iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			if !cfg.Enabled {
				// Immediate pass-through, zero allocations
				for item, err := range up {
					if !yield(item, err) {
						return
					}
				}
				return
			}

			var buf bytes.Buffer
			for item, err := range up {
				if err != nil {
					var zero T
					yield(zero, err)
					return
				}

				switch v := any(item).(type) {
				case FileContent:
					buf.Write(v.Content)
				case []byte:
					buf.Write(v)
				case string:
					buf.WriteString(v)
				}

				if !yield(item, nil) {
					return
				}
			}

			if buf.Len() > 0 {
				// Windows syscall.StringToUTF16 rzuca panic przy bajcie NUL (\x00)
				text := buf.String()
				if strings.IndexByte(text, 0) >= 0 {
					text = strings.ReplaceAll(text, "\x00", "")
				}
				_ = clipboard.WriteAll(text)
			}
		}
	}
}

func ClipboardOperator() action.NamedOperator {
	return action.NewOperator("clipboard.write", ClipboardWrite[FileContent])
}
