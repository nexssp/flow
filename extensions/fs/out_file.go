package fs

import (
	"bufio"
	"fmt"
	"iter"
	"os"
	"path/filepath"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// OutFileConfig controls out.file.
type OutFileConfig struct {
	Path      string `json:"path"       cli:"o,output"`
	Append    bool   `json:"append"     cli:"append"`
	FlushEach bool   `json:"flush_each"`
}

// OutFile aggregates the whole stream into a single file. Content items
// (FileContent) are written byte-for-byte; everything else uses
// fmt.Sprint or Stringer.
func OutFile[T any](cfg OutFileConfig) action.StreamOp[T, T] {
	return func(up iter.Seq2[T, error]) iter.Seq2[T, error] {
		return func(yield func(T, error) bool) {
			if cfg.Path == "" || cfg.Path == "-" {
				for item, err := range up {
					if !yield(item, err) {
						return
					}
				}
				return
			}

			file, openErr := openOutFile(cfg)
			if openErr != nil {
				var zero T
				yield(zero, openErr)
				return
			}

			err := writeOutStream(file, up, cfg.FlushEach, yield)
			if err != nil {
				var zero T
				yield(zero, err)
			}
		}
	}
}

func OutFileOperator() action.NamedOperator {
	return action.NewOperator("out.file", OutFile[FileContent])
}

func openOutFile(cfg OutFileConfig) (*os.File, error) {
	if dir := filepath.Dir(cfg.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, xerr.Internal("out.file: mkdir", err)
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if cfg.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(cfg.Path, flags, 0o600)
	if err != nil {
		return nil, xerr.Internal("out.file: open", err)
	}
	return file, nil
}

func writeOutStream[T any](
	file *os.File,
	up iter.Seq2[T, error],
	flushEach bool,
	yield func(T, error) bool,
) error {
	writer := bufio.NewWriterSize(file, 64*1024)
	defer func() { _ = writer.Flush(); _ = file.Close() }()

	for item, itemErr := range up {
		if itemErr != nil {
			return itemErr
		}
		data := itemBytes(item)
		if len(data) > 0 {
			if _, err := writer.Write(data); err != nil {
				return xerr.Internal("out.file: write", err)
			}
			if flushEach {
				if err := writer.Flush(); err != nil {
					return xerr.Internal("out.file: flush", err)
				}
			}
		}
		if !yield(item, nil) {
			return nil
		}
	}
	return nil
}

// itemBytes converts a stream item into the byte slice written to
// disk. FileContent and raw []byte are written byte-for-byte.
func itemBytes(item any) []byte {
	switch v := item.(type) {
	case FileContent:
		return v.Content
	case []byte:
		return v
	case string:
		return []byte(v)
	case fmt.Stringer:
		return []byte(v.String())
	default:
		return []byte(fmt.Sprint(v))
	}
}
